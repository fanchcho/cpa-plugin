package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

var updateRegisterGolden = flag.Bool("update-register-golden", false, "rewrite testdata/register.golden.json from the current registration payload")

// registerGoldenPath is the frozen contract snapshot for plugin.register.
// DESIGN §9 requires a before/after diff proving the SDK upgrade changed only
// two things: schema_version (1 -> 6) and the newly advertised capabilities.
const registerGoldenPath = "testdata/register.golden.json"

// registerPayloadJSON renders the registration response exactly as the host
// receives it (the envelope's result body).
func registerPayloadJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := handleMethod(pluginabi.MethodPluginRegister, mustJSON(map[string]any{"config_yaml": ""}))
	if err != nil {
		t.Fatalf("plugin.register: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode register envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("register envelope not ok: %s", raw)
	}
	return env.Result
}

// TestRegisterGoldenMatchesContract diffs the live registration payload against
// the frozen snapshot, allowing only the two expected classes of change.
func TestRegisterGoldenMatchesContract(t *testing.T) {
	got := decodeRegisterMap(t, registerPayloadJSON(t))

	if *updateRegisterGolden {
		raw, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(registerGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(registerGoldenPath, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s", registerGoldenPath)
		return
	}

	raw, err := os.ReadFile(registerGoldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update-register-golden to create): %v", err)
	}
	want := decodeRegisterMap(t, raw)

	// 1) The top-level key set is fixed.
	if diff := keySetDiff(want, got); len(diff) != 0 {
		t.Fatalf("top-level registration keys changed: %v", diff)
	}
	// 2) metadata must be identical apart from the version, which tracks VERSION
	// (TEST_PLAN A2-05: frozen or ignored, never a hard failure on a bump).
	if v := versionString(want); v != versionString(got) {
		t.Logf("metadata version moved with VERSION: %q -> %q", v, versionString(got))
	}
	if !equalJSON(t, dropVersionKeys(want["metadata"]), dropVersionKeys(got["metadata"])) {
		t.Fatalf("metadata changed beyond version:\nwant %v\ngot  %v", dropVersionKeys(want["metadata"]), dropVersionKeys(got["metadata"]))
	}
	// 3) capabilities: existing booleans may not flip, only new keys may appear
	// as true.
	wantCaps, _ := want["capabilities"].(map[string]any)
	gotCaps, _ := got["capabilities"].(map[string]any)
	for key, wantValue := range wantCaps {
		gotValue, present := gotCaps[key]
		if !present {
			t.Fatalf("capability %q disappeared from the registration payload", key)
		}
		if !equalJSON(t, wantValue, gotValue) {
			t.Fatalf("capability %q changed: %v -> %v", key, wantValue, gotValue)
		}
	}

	// 4) A2-02: schema_version is part of the contract, not decoration. The
	// baseline was frozen at the v7.2.30 value (1); the live payload must track
	// the pinned SDK constant, which must itself be 6 for CPA v7.3.5. Comparing
	// against the constant (not a literal) keeps this honest if the pin moves.
	wantSchema, okWant := want["schema_version"].(float64)
	if !okWant {
		t.Fatalf("golden is missing schema_version: %v", want["schema_version"])
	}
	gotSchema, okGot := got["schema_version"].(float64)
	if !okGot {
		t.Fatalf("live registration is missing schema_version: %v", got["schema_version"])
	}
	if uint32(gotSchema) != pluginabi.SchemaVersion {
		t.Fatalf("schema_version = %v, want pluginabi.SchemaVersion (%d)", gotSchema, pluginabi.SchemaVersion)
	}
	if pluginabi.SchemaVersion != 6 {
		t.Fatalf("pinned SDK SchemaVersion = %d, want 6 for CPA v7.3.5", pluginabi.SchemaVersion)
	}
	if uint32(wantSchema) == uint32(gotSchema) {
		t.Fatalf("schema_version did not change from the frozen baseline (%v): the golden file is not a v7.2.30 before-snapshot", wantSchema)
	}

	// 5) A2-06: the set of NEW capability keys has an upper bound. Without this,
	// any third capability could sneak into the payload and still "pass" the
	// per-key loop above (DESIGN §9: any other change is a regression).
	added := addedKeys(wantCaps, gotCaps)
	allowed := map[string]bool{"quota_provider": true, "request_lifecycle_plugin": true}
	for key := range added {
		if !allowed[key] {
			t.Fatalf("unexpected new capability %q (allowed: quota_provider, request_lifecycle_plugin)", key)
		}
	}
	for key := range allowed {
		if !added[key] {
			t.Fatalf("capability %q was not added by the SDK upgrade; payload=%v", key, gotCaps)
		}
	}
	// A2-04: the WebSocket observer is a PRD N1 non-goal — it must stay absent,
	// not merely false, so the host never wires an observer that cannot fire.
	if _, present := gotCaps["websocket_response_observer"]; present {
		t.Fatalf("capabilities.websocket_response_observer must not be advertised: %v", gotCaps["websocket_response_observer"])
	}
}

// addedKeys returns the keys present in got but absent from want.
func addedKeys(want, got map[string]any) map[string]bool {
	added := make(map[string]bool)
	for key := range got {
		if _, present := want[key]; !present {
			added[key] = true
		}
	}
	return added
}

// TestRegisterAdvertisesSchemaSixAndNewCapabilities is the explicit, readable
// statement of the two allowed changes (AC-2 / DESIGN §9 baseline diff).
func TestRegisterAdvertisesSchemaSixAndNewCapabilities(t *testing.T) {
	got := decodeRegisterMap(t, registerPayloadJSON(t))

	schema, ok := got["schema_version"].(float64)
	if !ok {
		t.Fatalf("schema_version missing or not numeric: %v", got["schema_version"])
	}
	// Compare against the SDK constant rather than a magic number so the
	// assertion tracks the pinned SDK (v7.3.5 => 6).
	if uint32(schema) != pluginabi.SchemaVersion {
		t.Fatalf("schema_version = %v, want pluginabi.SchemaVersion (%d)", schema, pluginabi.SchemaVersion)
	}
	if pluginabi.SchemaVersion != 6 {
		t.Fatalf("pinned SDK SchemaVersion = %d, want 6 for CPA v7.3.5", pluginabi.SchemaVersion)
	}

	caps, _ := got["capabilities"].(map[string]any)
	for _, key := range []string{"quota_provider", "request_lifecycle_plugin"} {
		if caps[key] != true {
			t.Fatalf("capabilities.%s = %v, want true", key, caps[key])
		}
	}
	// PRD N1 non-goal: the WebSocket observer must never be advertised — both
	// upstreams are HTTP SSE, so it would never fire.
	if value, present := caps["websocket_response_observer"]; present && value == true {
		t.Fatalf("capabilities.websocket_response_observer must not be advertised: %v", value)
	}
}

// TestRegisterAdvertisesOnlyImplementedCapabilities ties each advertised
// capability to a dispatch path that exists, so the payload cannot drift ahead
// of handleMethod.
func TestRegisterAdvertisesOnlyImplementedCapabilities(t *testing.T) {
	got := decodeRegisterMap(t, registerPayloadJSON(t))
	caps, _ := got["capabilities"].(map[string]any)

	required := map[string][]string{
		"quota_provider":           {pluginabi.MethodQuotaIdentifier, pluginabi.MethodQuotaDescribe, pluginabi.MethodQuotaFetch, pluginabi.MethodQuotaReset},
		"request_lifecycle_plugin": {pluginabi.MethodRequestComplete},
	}
	for capability, methods := range required {
		if caps[capability] != true {
			continue
		}
		for _, method := range methods {
			// An empty (but well-formed) request is enough: the point is that the
			// method is dispatched rather than falling into the unknown_method
			// default.
			out, err := handleMethod(method, mustJSON(map[string]any{}))
			if err != nil {
				t.Fatalf("capability %q advertises %s but dispatch errors: %v", capability, method, err)
			}
			if strings.Contains(string(out), `"unknown_method"`) {
				t.Fatalf("capability %q advertises %s but it is not wired into handleMethod: %s", capability, method, out)
			}
		}
	}
	// plugin.quiesce is not gated behind a capability flag; it must still be
	// dispatched, otherwise the host only logs a Debug "unsupported" fallback.
	resetQuiesceForTest()
	out, err := handleMethod(pluginabi.MethodPluginQuiesce, nil)
	if err != nil {
		t.Fatalf("plugin.quiesce dispatch: %v", err)
	}
	if strings.Contains(string(out), `"unknown_method"`) {
		t.Fatalf("plugin.quiesce is not wired into handleMethod: %s", out)
	}
	resetQuiesceForTest()
}

func decodeRegisterMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode registration payload: %v", err)
	}
	return out
}

func keySetDiff(want, got map[string]any) []string {
	var diff []string
	for key := range want {
		if _, present := got[key]; !present {
			diff = append(diff, "missing:"+key)
		}
	}
	for key := range got {
		if _, present := want[key]; !present {
			diff = append(diff, "extra:"+key)
		}
	}
	return diff
}

func versionString(doc map[string]any) string {
	meta, _ := doc["metadata"].(map[string]any)
	for key, value := range meta {
		// pluginapi.Metadata serializes as "Version" (no json tag); match
		// case-insensitively so this check is never a silent no-op.
		if strings.EqualFold(key, "version") {
			if text, ok := value.(string); ok {
				return text
			}
		}
	}
	return ""
}

// dropVersionKeys returns value without any case variant of "version": the
// field tracks the VERSION file, so a bump must not fail the metadata diff
// (TEST_PLAN A2-05).
func dropVersionKeys(value any) map[string]any {
	asMap, _ := value.(map[string]any)
	out := make(map[string]any, len(asMap))
	for k, v := range asMap {
		if strings.EqualFold(k, "version") {
			continue
		}
		out[k] = v
	}
	return out
}

func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	rawA, errA := json.Marshal(a)
	rawB, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		t.Fatalf("marshal for comparison: %v %v", errA, errB)
	}
	return string(rawA) == string(rawB)
}
