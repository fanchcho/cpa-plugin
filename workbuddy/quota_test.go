package main

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// decodeEnvelopeResult unmarshals the result payload of a plugin envelope.
func decodeEnvelopeResult(t *testing.T, raw []byte, out any) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not ok: %+v", env.Error)
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		t.Fatalf("decode result: %v", err)
	}
}

func TestQuotaIdentifierMatchesProviderName(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodQuotaIdentifier, nil)
	if err != nil {
		t.Fatal(err)
	}
	var resp identifierResponse
	decodeEnvelopeResult(t, raw, &resp)
	// The host resolves quota providers by this identifier (falling back to the
	// plugin id and the auth provider id), so inventing a new string would break
	// auth resolution for quota lookups.
	if resp.Identifier != providerName {
		t.Fatalf("quota identifier = %q, want %q", resp.Identifier, providerName)
	}
}

func TestQuotaDescribeContracts(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodQuotaDescribe, nil)
	if err != nil {
		t.Fatal(err)
	}
	var resp pluginapi.QuotaDescribeResponse
	decodeEnvelopeResult(t, raw, &resp)
	if resp.SupportsReset {
		t.Fatal("SupportsReset must be false: ResetQuota never succeeds")
	}
	if resp.DisplayName == "" {
		t.Fatal("DisplayName must be set for the management UI")
	}
	if len(resp.SupportedProviders) != 1 || resp.SupportedProviders[0] != providerName {
		t.Fatalf("SupportedProviders = %v, want [%q]", resp.SupportedProviders, providerName)
	}
}

func TestQuotaResetNeverSucceeds(t *testing.T) {
	raw, err := handleMethod(pluginabi.MethodQuotaReset, nil)
	if err != nil {
		t.Fatal(err)
	}
	var resp pluginapi.QuotaResetResponse
	decodeEnvelopeResult(t, raw, &resp)
	if resp.Success {
		t.Fatal("ResetQuota must never report success (no upstream write allowed)")
	}
	if resp.Message == "" {
		t.Fatal("ResetQuota must explain why it failed")
	}
}

// TestQuotaResetIssuesNoHostCalls is the static half of A1-12: "恒 Success:false"
// is not enough — the handler must not even be able to reach upstream. A reset
// that silently POSTs a write and then reports failure would still corrupt
// provider-managed credits.
func TestQuotaResetIssuesNoHostCalls(t *testing.T) {
	body := functionBody(t, readPackageSourceFile(t, "quota.go"), "func handleQuotaReset()")
	for _, forbidden := range []string{
		"hostCall(",
		"hostHTTPDo",
		"billingCall",
		"http.NewRequest",
		"quotaCreditsFetch(",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("handleQuotaReset must not touch upstream but references %q", forbidden)
		}
	}
}

// TestQuotaFetchForwardsHostCallbackID pins ADR-001 at the unit level: the
// callback id the host put on the quota.fetch envelope must be the one handed
// to the credits fetch (which routes it into host.http.do). A plugin-minted id
// would fight the host's callback-nesting avoidance.
func TestQuotaFetchForwardsHostCallbackID(t *testing.T) {
	prev := quotaCreditsFetch
	defer func() { quotaCreditsFetch = prev }()

	var gotCallback string
	var gotAuthID string
	quotaCreditsFetch = func(authID string, _ *storedAuth, force bool, callbackID string) (string, *checkinSummary, *creditsSummary, []string) {
		gotAuthID = authID
		gotCallback = callbackID
		if force {
			t.Fatal("a quota fetch must not force-refresh past the credits cache")
		}
		return "Pro", nil, &creditsSummary{TotalRemain: 5, TotalUsed: 5, TotalSize: 10}, nil
	}

	rawAuth := mustJSON(map[string]any{
		"auth":    map[string]any{"accessToken": "test-token", "domain": "codebuddy.cn"},
		"account": map[string]any{"uid": "uid-1"},
	})
	req := mustJSON(map[string]any{
		"auth_index":       "0",
		"auth_id":          "auth-cb",
		"provider":         providerName,
		"storage_json":     base64.StdEncoding.EncodeToString(rawAuth),
		"host_callback_id": "cb-host-42",
	})
	out, err := handleMethod(pluginabi.MethodQuotaFetch, req)
	if err != nil {
		t.Fatal(err)
	}
	var resp pluginapi.QuotaFetchResponse
	decodeEnvelopeResult(t, out, &resp)
	if gotCallback != "cb-host-42" {
		t.Fatalf("callback id passed to the credits fetch = %q, want the envelope's host_callback_id", gotCallback)
	}
	if gotAuthID != "auth-cb" {
		t.Fatalf("auth id = %q, want auth-cb", gotAuthID)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].Buckets[0].RemainingFraction != 0.5 {
		t.Fatalf("quota response = %+v, want one bucket at 0.5", resp.Groups)
	}
}

func TestQuotaBucketFromCreditsFraction(t *testing.T) {
	cr := &creditsSummary{TotalRemain: 75, TotalUsed: 25, TotalSize: 100}
	bucket, ok := quotaBucketFromCredits(cr)
	if !ok {
		t.Fatal("bucket must exist when the pool size is known")
	}
	if math.Abs(bucket.RemainingFraction-0.75) > 1e-9 {
		t.Fatalf("RemainingFraction = %v, want 0.75", bucket.RemainingFraction)
	}
}

func TestQuotaBucketFromCreditsZeroSizeIsNotNaN(t *testing.T) {
	// Legacy / partially-reported auth: size <= 0 must never produce NaN or Inf,
	// which the management UI would render literally as "NaN".
	cr := &creditsSummary{TotalRemain: 5, TotalUsed: 0, TotalSize: 0}
	bucket, ok := quotaBucketFromCredits(cr)
	if !ok {
		t.Fatal("bucket must exist when credits are present")
	}
	if math.IsNaN(bucket.RemainingFraction) || math.IsInf(bucket.RemainingFraction, 0) {
		t.Fatalf("RemainingFraction = %v, want a finite number", bucket.RemainingFraction)
	}
	if bucket.RemainingFraction != 1.0 {
		t.Fatalf("RemainingFraction = %v, want 1.0 for an unknown pool", bucket.RemainingFraction)
	}
}

func TestQuotaBucketFromCreditsEmptyPoolOmitsBucket(t *testing.T) {
	// Nothing reported at all: omit the bucket rather than invent a ratio.
	if _, ok := quotaBucketFromCredits(&creditsSummary{}); ok {
		t.Fatal("empty credits must not emit a bucket")
	}
}

func TestQuotaBucketFractionClampsOutOfRange(t *testing.T) {
	cr := &creditsSummary{TotalRemain: 150, TotalUsed: 0, TotalSize: 100}
	bucket, ok := quotaBucketFromCredits(cr)
	if !ok {
		t.Fatal("bucket must exist")
	}
	if bucket.RemainingFraction != 1.0 {
		t.Fatalf("RemainingFraction = %v, want clamped 1.0", bucket.RemainingFraction)
	}
}

func TestBuildQuotaFetchResponseMapsSummaryAndSubscription(t *testing.T) {
	cr := &creditsSummary{
		TotalRemain: 1234,
		TotalUsed:   266,
		TotalSize:   1500,
		PackCount:   1,
		Packages: []packageSummary{{
			Name:     "CodeBuddy个人体验版",
			CycleEnd: "2999-01-01 00:00:00",
		}},
	}
	resp := buildQuotaFetchResponse(cr, "Pro")

	if resp.Subscription == nil || resp.Subscription.Plan != "Pro" {
		t.Fatalf("Subscription = %+v, want plan Pro", resp.Subscription)
	}
	if len(resp.Summary) != 2 {
		t.Fatalf("Summary = %+v, want 2 metrics", resp.Summary)
	}
	for _, metric := range resp.Summary {
		if metric.Unit != "credits" {
			t.Fatalf("metric %q unit = %q, want credits", metric.Key, metric.Unit)
		}
		if strings.TrimSpace(metric.Key) == "" {
			t.Fatalf("metric key must be stable/non-empty: %+v", metric)
		}
	}
	if len(resp.Groups) != 1 || len(resp.Groups[0].Buckets) != 1 {
		t.Fatalf("Groups = %+v, want one group with one bucket", resp.Groups)
	}
	if resp.Groups[0].Buckets[0].ResetTime == "" {
		t.Fatal("ResetTime must be set when a future cycle end exists")
	}
	if _, err := timeParseQuota(resp.Groups[0].Buckets[0].ResetTime); err != nil {
		t.Fatalf("ResetTime %q is not RFC3339: %v", resp.Groups[0].Buckets[0].ResetTime, err)
	}
}

func TestBuildQuotaFetchResponseOmitsSubscriptionWhenPlanEmpty(t *testing.T) {
	resp := buildQuotaFetchResponse(&creditsSummary{TotalRemain: 1, TotalSize: 1}, "")
	if resp.Subscription != nil {
		t.Fatalf("Subscription = %+v, want nil for an unknown plan", resp.Subscription)
	}
}

func TestQuotaResetTimeSkipsPastCycleEnds(t *testing.T) {
	cr := &creditsSummary{Packages: []packageSummary{
		{CycleEnd: "2000-01-01 00:00:00"},
		{CycleEnd: "2999-06-01 12:00:00"},
		{CycleEnd: "2999-12-01 12:00:00"},
	}}
	got := quotaResetTime(cr)
	want := "2999-06-01T12:00:00Z"
	if got != want {
		t.Fatalf("quotaResetTime = %q, want the earliest future cycle end %q", got, want)
	}
}

// TestQuotaFetchReusesCreditsCache is the AC-4-adjacent guarantee: a quota query
// must NOT bypass the credits cache and stampede the billing API (PRD 5.2 hard
// constraint 3). Two consecutive fetches must hit upstream at most once per
// endpoint.
func TestQuotaFetchReusesCreditsCache(t *testing.T) {
	resetAccountCacheForTest()
	var billingHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		billingHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/get-user-resource"):
			// billingCall returns the envelope's `data`; the caller then unwraps
			// a "Response" -> "Data" nesting for this endpoint.
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"Response":{"Data":{"TotalCount":1,"TotalDosage":1000,"Accounts":[{"PackageName":"Pack","CycleCapacityRemain":800,"CycleCapacityUsed":200,"CycleCapacitySize":1000,"CycleEndTime":"2999-01-01 00:00:00"}]}}}}`))
		case strings.HasSuffix(r.URL.Path, "/checkin-activity-status"):
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"active":true,"today_checked_in":false}}`))
		case strings.HasSuffix(r.URL.Path, "/get-payment-type"):
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"paymentType":"Pro"}}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{}}`))
		}
	}))
	defer srv.Close()
	restore := setBillingBase(srv.URL)
	defer restore()

	rawAuth := mustJSON(map[string]any{
		"auth":    map[string]any{"accessToken": "test-token", "domain": "codebuddy.cn"},
		"account": map[string]any{"uid": "uid-1", "nickname": "A&B"},
	})
	// The host marshals StorageJSON ([]byte) as base64 on the wire — the same
	// encoding the real host adapter produces from the auth file bytes.
	encodedAuth := base64.StdEncoding.EncodeToString(rawAuth)

	fetch := func() pluginapi.QuotaFetchResponse {
		req := mustJSON(map[string]any{
			"auth_index":   "0",
			"auth_id":      "auth-1",
			"provider":     providerName,
			"storage_json": encodedAuth,
		})
		out, err := handleMethod(pluginabi.MethodQuotaFetch, req)
		if err != nil {
			t.Fatal(err)
		}
		var resp pluginapi.QuotaFetchResponse
		decodeEnvelopeResult(t, out, &resp)
		return resp
	}

	first := fetch()
	if len(first.Groups) != 1 || len(first.Groups[0].Buckets) != 1 {
		t.Fatalf("first fetch groups = %+v", first.Groups)
	}
	if got := first.Groups[0].Buckets[0].RemainingFraction; math.Abs(got-0.8) > 1e-9 {
		t.Fatalf("remainingFraction = %v, want 0.8", got)
	}
	if first.Subscription == nil || first.Subscription.Plan != "Pro" {
		t.Fatalf("subscription = %+v, want Pro", first.Subscription)
	}
	afterFirst := billingHits.Load()
	if afterFirst == 0 {
		t.Fatal("expected at least one upstream billing call")
	}

	_ = fetch()
	if got := billingHits.Load(); got != afterFirst {
		t.Fatalf("second fetch hit upstream again (%d -> %d): quota must reuse the credits cache", afterFirst, got)
	}
}

// TestQuotaFetchFailureIsSanitized ensures upstream failures surface as a fixed
// message with no upstream body leakage (PRD 7.3).
func TestQuotaFetchFailureIsSanitized(t *testing.T) {
	resetAccountCacheForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"secret":"upstream-detail-should-not-leak"}`))
	}))
	defer srv.Close()
	restore := setBillingBase(srv.URL)
	defer restore()

	rawAuth := mustJSON(map[string]any{
		"auth":    map[string]any{"accessToken": "test-token", "domain": "codebuddy.cn"},
		"account": map[string]any{"uid": "uid-1", "nickname": "A&B"},
	})
	req := mustJSON(map[string]any{
		"auth_index":   "0",
		"auth_id":      "auth-2",
		"provider":     providerName,
		"storage_json": base64.StdEncoding.EncodeToString(rawAuth),
	})
	out, err := handleMethod(pluginabi.MethodQuotaFetch, req)
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if env.OK {
		t.Fatalf("expected a failure envelope, got %s", out)
	}
	if strings.Contains(env.Error.Message, "upstream-detail-should-not-leak") {
		t.Fatalf("error message leaked upstream body: %q", env.Error.Message)
	}
	if env.Error.HTTPStatus == 0 {
		t.Fatalf("failure must carry an http_status: %+v", env.Error)
	}
}

func TestQuotaFetchWithoutCredentialFails(t *testing.T) {
	out, err := handleMethod(pluginabi.MethodQuotaFetch, mustJSON(map[string]any{"auth_index": "", "provider": providerName}))
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if env.OK {
		t.Fatalf("expected failure without a credential, got %s", out)
	}
}

func timeParseQuota(raw string) (int64, error) {
	parsed, ok := parseQuotaTime(raw)
	if !ok {
		return 0, &quotaParseError{raw: raw}
	}
	return parsed.Unix(), nil
}

type quotaParseError struct{ raw string }

func (e *quotaParseError) Error() string { return "unparseable quota time: " + e.raw }
