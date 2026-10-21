// quota.go implements the pluginapi.QuotaProvider surface for WorkBuddy
// (CPA v7.3.5 contract, DESIGN §4).
//
// It is a PARALLEL channel next to the existing management routes: the panel
// keeps using /v0/management/plugins/workbuddy/credits because QuotaFetchResponse
// cannot carry the package/checkin/region richness the panel renders
// (PRD 5.2 hard constraint 2). FetchQuota deliberately reuses the existing
// credits cache + singleflight + limiter path, so a quota query never bypasses
// the TTL and hammers the billing API (PRD 5.2 hard constraint 3).
package main

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// quotaDisplayName is the user-facing label shown by the management UI for this
// plugin's quota provider. Kept separate from providerName (the routing key) so
// a label change can never alter quota/auth matching.
const quotaDisplayName = "WorkBuddy"

// quotaFetchWire mirrors the host's rpcQuotaFetchRequest: the pluginapi request
// plus the host callback id the plugin must echo on its nested host calls.
type quotaFetchWire struct {
	pluginapi.QuotaFetchRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

// quotaIdentifier responds to quota.identifier. It MUST return the existing
// providerName constant: the host matches QuotaProvider.Identifier() → plugin
// ID → AuthProvider.Identifier() and falling back to SupportedProviders
// (internal/pluginhost/quota_provider.go), so inventing a new identifier would
// stop quota lookups from resolving to this plugin's auths.
func handleQuotaIdentifier() ([]byte, error) {
	return okEnvelope(identifierResponse{Identifier: providerName})
}

// handleQuotaDescribe responds to quota.describe.
func handleQuotaDescribe() ([]byte, error) {
	return okEnvelope(pluginapi.QuotaDescribeResponse{
		DisplayName:        quotaDisplayName,
		SupportedProviders: []string{providerName},
		// WorkBuddy credits are provider-managed: ResetQuota always fails, and
		// SupportsReset:false keeps the management UI from offering a reset
		// button that could never succeed (DESIGN §4.3).
		SupportsReset: false,
	})
}

// quotaCreditsFetch is the credits fetch the quota path reuses. Declared as a
// var so tests can assert the ADR-001 callback passthrough (the id we hand to
// the host equals the one the host put on this RPC) without a live host bridge.
var quotaCreditsFetch = cachedAccountDetailsWithCallback

// handleQuotaFetch responds to quota.fetch.
//
// Transport note: the HostHTTPClient interface field on QuotaFetchRequest is
// `json:"-"` and is additionally nil'd by the host adapter's
// sanitizePluginRequest before the RPC reaches us, so the plugin cannot call it
// directly. The sanctioned equivalent is the host-provided callback id: routing
// through hostHTTPDoWithCallback(req, req.HostCallbackID) re-enters the same
// callback context the host opened for this quota call (which carries the auth
// and its transport policy). We never mint our own callback id.
func handleQuotaFetch(raw []byte) ([]byte, error) {
	var req quotaFetchWire
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	sa, err := quotaAuthFromRequest(req)
	if err != nil {
		return errorEnvelopeWithStatus("quota_unavailable", "credential unavailable", 502), nil
	}
	// cachedAccountDetailsWithCallback reuses the 45s TTL cache, the per-authID
	// singleflight and the userResourceSlots limiter that the panel path uses.
	// force=false: a quota query must not bypass the cache and stampede upstream.
	// The callback id is the one the host opened for THIS call (ADR-001): we
	// pass it through verbatim and never mint our own.
	plan, _, cr, _ := quotaCreditsFetch(req.AuthID, sa, false, req.HostCallbackID)
	if cr == nil {
		return errorEnvelopeWithStatus("quota_upstream_unavailable", "upstream billing unavailable", 502), nil
	}
	return okEnvelope(buildQuotaFetchResponse(cr, plan))
}

// quotaAuthFromRequest resolves the credential for a quota fetch. The host
// backfills StorageJSON from the auth file (authPhysicalJSONByIndex) so the
// cached path can run without an extra host round-trip; we only fall back to
// host.auth.get when the host could not resolve a physical file.
func quotaAuthFromRequest(req quotaFetchWire) (*storedAuth, error) {
	if len(req.StorageJSON) > 0 {
		if sa, err := parseStored(req.StorageJSON); err == nil {
			return sa, nil
		}
	}
	authIndex := strings.TrimSpace(req.AuthIndex)
	if authIndex == "" {
		return nil, errors.New("quota credential unavailable: no storage and no auth_index")
	}
	return hostAuthGet(authIndex)
}

// buildQuotaFetchResponse maps an existing creditsSummary onto the normalized
// QuotaFetchResponse (DESIGN §4.4 field mapping).
func buildQuotaFetchResponse(cr *creditsSummary, plan string) pluginapi.QuotaFetchResponse {
	if cr == nil {
		return pluginapi.QuotaFetchResponse{}
	}
	resp := pluginapi.QuotaFetchResponse{
		Summary: []pluginapi.QuotaMetric{
			// Key/Label are stable identifiers, not localized text: management
			// clients may key on them, so they must not drift with language.
			{Key: "credits_remaining", Label: "Credits remaining", Value: float64(cr.TotalRemain), Unit: "credits", Format: "number"},
			{Key: "credits_used", Label: "Credits used", Value: float64(cr.TotalUsed), Unit: "credits", Format: "number"},
		},
	}
	if p := strings.TrimSpace(plan); p != "" {
		resp.Subscription = &pluginapi.QuotaSubscription{Plan: p}
	}
	bucket, ok := quotaBucketFromCredits(cr)
	if !ok {
		return resp
	}
	resp.Groups = []pluginapi.QuotaGroup{{
		DisplayName: quotaGroupDisplayName(cr),
		Buckets:     []pluginapi.QuotaBucket{bucket},
	}}
	return resp
}

// quotaBucketFromCredits derives the single normalized bucket. The division is
// guarded: a zero/negative pool (legacy or partially-reported auths) yields
// RemainingFraction 1.0 rather than NaN/Inf, which would render as "NaN" in the
// management UI (PRD 5.2 / API_DESIGN Q3).
func quotaBucketFromCredits(cr *creditsSummary) (pluginapi.QuotaBucket, bool) {
	if cr.TotalSize <= 0 {
		if cr.TotalRemain == 0 && cr.TotalUsed == 0 {
			// Nothing to describe: omit the bucket instead of inventing a ratio.
			return pluginapi.QuotaBucket{}, false
		}
		return pluginapi.QuotaBucket{
			Window:            "cycle",
			RemainingFraction: 1.0,
			Description:       "pool size unavailable",
		}, true
	}
	fraction := float64(cr.TotalRemain) / float64(cr.TotalSize)
	if math.IsNaN(fraction) || math.IsInf(fraction, 0) {
		fraction = 1.0
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	bucket := pluginapi.QuotaBucket{
		Window:            "cycle",
		RemainingFraction: fraction,
	}
	if reset := quotaResetTime(cr); reset != "" {
		// RFC3339 string; the host does not parse it (API_DESIGN Q4).
		bucket.ResetTime = reset
	}
	return bucket, true
}

// quotaResetTime picks the nearest future cycle end across packages.
func quotaResetTime(cr *creditsSummary) string {
	now := time.Now()
	var best time.Time
	for _, p := range cr.Packages {
		raw := strings.TrimSpace(p.CycleEnd)
		if raw == "" {
			continue
		}
		parsed, ok := parseQuotaTime(raw)
		if !ok {
			// Unparseable upstream timestamps are passed through verbatim so no
			// information is lost; the host treats the value as opaque.
			if best.IsZero() {
				best = time.Time{}
				return raw
			}
			continue
		}
		if parsed.Before(now) {
			continue
		}
		if best.IsZero() || parsed.Before(best) {
			best = parsed
		}
	}
	if best.IsZero() {
		return ""
	}
	return best.UTC().Format(time.RFC3339)
}

// parseQuotaTime accepts the layouts the CodeBuddy billing API is known to use
// (space-separated local time and RFC3339).
func parseQuotaTime(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// quotaGroupDisplayName names the bucket group for the management UI. Enterprise
// accounts report a single synthetic package, so their group is labelled as
// such; everything else uses the provider label.
func quotaGroupDisplayName(cr *creditsSummary) string {
	if cr.PackCount == 1 && len(cr.Packages) == 1 && cr.Packages[0].Name == "Enterprise" {
		return "Enterprise"
	}
	return quotaDisplayName
}

// handleQuotaReset responds to quota.reset. It never touches upstream: WorkBuddy
// credits are managed by the provider, and a reset write is out of scope
// (PRD 7.4). The envelope is ok=true carrying success:false so the host does not
// treat it as a transport failure.
func handleQuotaReset() ([]byte, error) {
	return okEnvelope(pluginapi.QuotaResetResponse{Success: false, Message: "provider-managed"})
}
