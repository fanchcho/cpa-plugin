package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// TestAffinityQueryWithoutSessionSkipsLookup covers VC-3 item 4: with no
// session identity the panel must not issue a lookup at all (the host rejects
// requests with a missing session_id).
func TestAffinityQueryWithoutSessionSkipsLookup(t *testing.T) {
	out := handleAffinityQuery(pluginapi.ManagementRequest{Query: map[string][]string{
		"model": {"m"},
	}})
	if out["supported"] != false {
		t.Fatalf("supported = %v, want false without a session identity", out["supported"])
	}
	if _, present := out["auth_index"]; present {
		t.Fatalf("unbound response must not carry auth_index: %v", out)
	}
}

// TestAffinityLookupFailsClosedWithoutHost ensures a missing host bridge is a
// silent degrade, not an error.
func TestAffinityLookupFailsClosedWithoutHost(t *testing.T) {
	if _, ok := affinityLookup(providerName, "m", "sess-1"); ok {
		t.Fatal("lookup must report !ok when the host bridge is unavailable")
	}
}

// TestAffinityProbeAlwaysUsesHTTP200 pins the advisory semantics: the affinity
// field must never surface a 5xx to the panel (VC-3 item 1).
func TestAffinityProbeAlwaysUsesHTTP200(t *testing.T) {
	if affinityProbeStatus != http.StatusOK {
		t.Fatalf("affinityProbeStatus = %d, want 200", affinityProbeStatus)
	}
}

// TestAffinityQueryOnlyExposesBindingWhenBound verifies the response shape: for
// unbound/ambiguous the plugin must not hand out an auth_index, which the panel
// would render as a binding that does not exist.
func TestAffinityQueryOnlyExposesBindingWhenBound(t *testing.T) {
	for _, status := range []string{
		pluginapi.HostAffinityStatusUnbound,
		pluginapi.HostAffinityStatusAmbiguous,
		pluginapi.HostAffinityStatusUnsupported,
	} {
		resp := affinityPanelPayload(status, "7")
		if _, present := resp["auth_index"]; present {
			t.Fatalf("status %q must not expose auth_index: %v", status, resp)
		}
	}
	bound := affinityPanelPayload(pluginapi.HostAffinityStatusBound, "7")
	if bound["auth_index"] != "7" {
		t.Fatalf("bound response = %v, want auth_index 7", bound)
	}
}

// TestAffinityLookupRequestRequiresAllFields documents the host-side contract we
// must satisfy: provider, model and session_id are all mandatory.
func TestAffinityLookupRequestRequiresAllFields(t *testing.T) {
	raw := mustJSON(pluginapi.HostAffinityLookupRequest{
		Provider:  providerName,
		Model:     "m",
		SessionID: "sess-1",
	})
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"provider", "model", "session_id"} {
		if value, _ := decoded[key].(string); strings.TrimSpace(value) == "" {
			t.Fatalf("affinity request must carry %q: %s", key, raw)
		}
	}
}

// TestPanelHasNoAffinityWritePath is the red-line guard: nothing in this package
// may call a host affinity mutator. The host exposes lookup only, and a plugin
// overriding host routing would fight the Scheduler.
func TestPanelHasNoAffinityWritePath(t *testing.T) {
	for _, name := range []string{"affinity.go", "management.go", "panel.html"} {
		src := readPackageSourceFile(t, name)
		for _, forbidden := range []string{"affinity.set", "affinity.write", "SetSessionAffinity", "BindSessionAffinity"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s references a forbidden affinity write path %q", name, forbidden)
			}
		}
		if strings.Contains(src, "MethodHostAffinitySet") {
			t.Fatalf("%s references a nonexistent affinity mutator method", name)
		}
	}
}
