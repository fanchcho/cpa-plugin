// affinity.go implements the read-only host.affinity.lookup consumption for
// QoderWork (DESIGN §7). Ported from the workbuddy contract.
//
// The host exposes which credential a downstream session is currently pinned to
// (internal/pluginhost/affinity_callbacks.go → LookupSessionAffinity). The panel
// uses it to explain "why did my request route to that other account?".
//
// RED LINE: this is an OBSERVATION ONLY. The affinity state belongs to the
// host's AuthManager/Scheduler; the plugin must never write it. There is no write
// path here and none may be added — overriding host routing from a provider
// plugin would conflict with Scheduler capabilities.
package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// affinityStatusUnknown is used when the panel did not supply a session
// identity: no lookup is attempted at all in that case.
const affinityStatusUnknown = "unknown"

// affinityLookup performs the host callback. Returns ok=false when the host does
// not support the call (older host, no auth manager) or errored — callers degrade
// silently (VC-3).
func affinityLookup(provider, model, sessionID string) (pluginapi.HostAffinityLookupResponse, bool) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	sessionID = strings.TrimSpace(sessionID)
	// The host rejects any missing field, so short-circuit before the RPC.
	if provider == "" || model == "" || sessionID == "" {
		return pluginapi.HostAffinityLookupResponse{Status: affinityStatusUnknown}, false
	}
	raw, err := hostCall(pluginabi.MethodHostAffinityLookup, mustJSON(pluginapi.HostAffinityLookupRequest{
		Provider:  provider,
		Model:     model,
		SessionID: sessionID,
	}))
	if err != nil {
		return pluginapi.HostAffinityLookupResponse{Status: affinityStatusUnknown}, false
	}
	result, err := hostBridgeUnwrap(raw, pluginabi.MethodHostAffinityLookup)
	if err != nil {
		return pluginapi.HostAffinityLookupResponse{Status: affinityStatusUnknown}, false
	}
	var resp pluginapi.HostAffinityLookupResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return pluginapi.HostAffinityLookupResponse{Status: affinityStatusUnknown}, false
	}
	if resp.Status == "" {
		resp.Status = affinityStatusUnknown
	}
	return resp, true
}

// handleAffinityQuery serves GET <base>/affinity for the panel.
//
// Query: ?session_id=&model=[&provider=]
// Response is deliberately minimal — only what the panel renders. A failed or
// unsupported lookup returns 200 with {"supported":false} rather than an error
// status, because this is a best-effort diagnostic field and the panel must not
// surface a red error block when it is unavailable (VC-3 / DESIGN §7.3).
func handleAffinityQuery(req pluginapi.ManagementRequest) map[string]any {
	sessionID := strings.TrimSpace(firstQueryValue(req, "session_id"))
	model := strings.TrimSpace(firstQueryValue(req, "model"))
	provider := strings.TrimSpace(firstQueryValue(req, "provider"))
	if provider == "" {
		provider = providerName
	}
	if sessionID == "" || model == "" {
		// No session identity → skip the query entirely (DESIGN §7.3 item 4).
		return map[string]any{"supported": false, "status": affinityStatusUnknown, "reason": "no_session_identity"}
	}
	resp, ok := affinityLookup(provider, model, sessionID)
	if !ok || resp.Status == pluginapi.HostAffinityStatusUnsupported {
		return map[string]any{"supported": false, "status": affinityStatusUnknown}
	}
	return affinityPanelPayload(resp.Status, resp.AuthIndex)
}

// affinityPanelPayload builds the panel-facing payload for an observed status.
// AuthIndex/Disabled/Unavailable are only meaningful for a bound session; emitting
// them for unbound/ambiguous would invite the panel to render a binding that does
// not exist, so they are dropped for every other status.
func affinityPanelPayload(status, authIndex string) map[string]any {
	out := map[string]any{
		"supported": true,
		"status":    status,
	}
	if status == pluginapi.HostAffinityStatusBound {
		out["auth_index"] = authIndex
	}
	return out
}

// firstQueryValue reads the first value of a query key without mutating the map.
func firstQueryValue(req pluginapi.ManagementRequest, key string) string {
	values := req.Query[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// affinitySupported reports whether this host advertises the affinity callback.
// A missing capability is not an error (VC-3).
func affinitySupported() bool {
	return hostBridgeAvailable()
}

// affinityProbeStatus is the HTTP status the affinity route always returns: the
// field is advisory, so transport-level failures must not surface as 5xx.
const affinityProbeStatus = http.StatusOK
