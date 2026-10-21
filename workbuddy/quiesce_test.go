package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestPluginQuiesceIsIdempotent(t *testing.T) {
	defer resetQuiesceForTest()
	restoreFlush := stubQuiesceFlush()
	defer restoreFlush()

	// Repeated calls must not panic (double close) and must stop timers.
	stopLoginStatesJanitor() // pre-stop so the quiesce path exercises the nil branch too
	pluginQuiesce()
	pluginQuiesce()
	pluginQuiesce()

	if !pluginDraining() {
		t.Fatal("quiesce must set the draining flag")
	}
}

func TestPluginQuiesceStopsLoginStatesJanitor(t *testing.T) {
	defer resetQuiesceForTest()
	restoreFlush := stubQuiesceFlush()
	defer restoreFlush()

	startLoginStatesJanitor()
	pluginQuiesce()
	// Stop must have released the channel; a second call is a no-op rather than
	// a double close (which would panic).
	stopLoginStatesJanitor()
}

// TestPluginQuiesceStopsBackgroundTimers proves the scheduler loop really exits:
// quiesce closes the stop channel, so ensureScheduler can start a fresh loop.
func TestPluginQuiesceStopsBackgroundTimers(t *testing.T) {
	defer resetQuiesceForTest()
	restoreFlush := stubQuiesceFlush()
	defer restoreFlush()

	ensureScheduler()
	schedulerMu.Lock()
	runningBefore := schedulerStop != nil
	schedulerMu.Unlock()
	if !runningBefore {
		t.Fatal("scheduler should be running after ensureScheduler")
	}

	pluginQuiesce()

	schedulerMu.Lock()
	stillRunning := schedulerStop != nil
	schedulerMu.Unlock()
	if stillRunning {
		t.Fatal("quiesce must close the scheduler stop channel")
	}
	// A stopped scheduler must be restartable (the replacement instance path).
	ensureScheduler()
	schedulerMu.Lock()
	restarted := schedulerStop != nil
	schedulerMu.Unlock()
	if !restarted {
		t.Fatal("scheduler must be restartable after quiesce")
	}
	stopCheckinScheduler()
}

func TestPluginQuiesceFlushesState(t *testing.T) {
	defer resetQuiesceForTest()
	var flushed int
	prev := quiesceModelFlush
	quiesceModelFlush = func() error { flushed++; return nil }
	defer func() { quiesceModelFlush = prev }()

	pluginQuiesce()
	if flushed != 1 {
		t.Fatalf("flush called %d times, want 1", flushed)
	}
	if notes := lastQuiesceFlush(); len(notes) != 0 {
		t.Fatalf("flush notes = %v, want none on success", notes)
	}
}

func TestPluginQuiesceReportsFlushFailureWithoutPanicking(t *testing.T) {
	defer resetQuiesceForTest()
	prev := quiesceModelFlush
	quiesceModelFlush = func() error { return errQuiesceFlushStub }
	defer func() { quiesceModelFlush = prev }()

	pluginQuiesce() // must not panic on a failed flush
	notes := lastQuiesceFlush()
	if len(notes) != 1 || !strings.Contains(notes[0], "model-catalog") {
		t.Fatalf("flush notes = %v, want one model-catalog failure", notes)
	}
}

// TestDrainingRejectsNewExecutorWork covers AC-5's fast-fail requirement: after
// quiesce, a new executor request is refused with HTTP 503 and a retryable body.
func TestDrainingRejectsNewExecutorWork(t *testing.T) {
	resetQuiesceForTest()
	prev := quiesceModelFlush
	quiesceModelFlush = func() error { return nil }
	pluginQuiesce()

	raw := mustJSON(map[string]any{
		"auth_id":      "auth-1",
		"model":        "m",
		"storage_json": "",
		"payload":      []byte(`{}`),
	})
	out, err := handleMethod(pluginabi.MethodExecutorExecute, raw)
	if err != nil {
		t.Fatal(err)
	}
	assertDrainingEnvelope(t, out)
	quiesceModelFlush = prev
	resetQuiesceForTest()
}

// TestDrainingRejectsManagementWrites covers the management half of the draining
// contract.
func TestDrainingRejectsManagementWrites(t *testing.T) {
	resetQuiesceForTest()
	prev := quiesceModelFlush
	quiesceModelFlush = func() error { return nil }
	pluginQuiesce()

	out, err := handleManagement(mustJSON(pluginapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   loadedManagementBasePath() + "/plugins/" + providerName + "/accounts",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK {
		t.Fatalf("management envelope not ok: %s", out)
	}
	var resp pluginapi.ManagementResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while draining", resp.StatusCode)
	}
	quiesceModelFlush = prev
	resetQuiesceForTest()
}

// TestDrainingKeepsPanelReachable ensures the panel HTML stays served during
// draining so an operator can still see state.
func TestDrainingKeepsPanelReachable(t *testing.T) {
	resetQuiesceForTest()
	prev := quiesceModelFlush
	quiesceModelFlush = func() error { return nil }
	pluginQuiesce()

	out, err := handleManagement(mustJSON(pluginapi.ManagementRequest{
		Method: http.MethodGet,
		Path:   loadedResourceBasePath() + "/panel",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK {
		t.Fatalf("panel envelope not ok while draining: %s", out)
	}
	var resp pluginapi.ManagementResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(resp.Body) == 0 {
		t.Fatalf("panel status=%d bodyLen=%d, want 200 with content", resp.StatusCode, len(resp.Body))
	}
	quiesceModelFlush = prev
	resetQuiesceForTest()
}

// assertDrainingEnvelope checks the plugin-side envelope carries both the 503
// status and the retryable marker. The literal downstream "retryable":true is
// produced from the JSON-valued Message (see drainErrorBody).
func assertDrainingEnvelope(t *testing.T, out []byte) {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if env.OK {
		t.Fatalf("draining request must fail: %s", out)
	}
	if env.Error == nil {
		t.Fatalf("draining request must carry an error: %s", out)
	}
	if env.Error.HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("http_status = %d, want 503", env.Error.HTTPStatus)
	}
	if !env.Error.Retryable {
		t.Fatalf("retryable = false, want true: %s", out)
	}
	var inner map[string]any
	if err := json.Unmarshal([]byte(env.Error.Message), &inner); err != nil {
		t.Fatalf("draining message must be valid JSON so the downstream body keeps retryable: %q", env.Error.Message)
	}
	errObj, _ := inner["error"].(map[string]any)
	if errObj["retryable"] != true {
		t.Fatalf("draining message = %q, want retryable:true inside", env.Error.Message)
	}
}

// stubQuiesceFlush replaces the flush hook with a no-op so quiesce tests never
// write into the developer's real config directory.
func stubQuiesceFlush() func() {
	prev := quiesceModelFlush
	quiesceModelFlush = func() error { return nil }
	return func() { quiesceModelFlush = prev }
}

var errQuiesceFlushStub = &quiesceFlushStubError{}

type quiesceFlushStubError struct{}

func (e *quiesceFlushStubError) Error() string { return "stub flush failure" }

// TestQuiesceMethodIsRegisteredAsHandled ensures handleMethod dispatches
// plugin.quiesce; a host that gets "unknown_method" only logs a Debug fallback
// and the timers would keep running after a hot replace (DESIGN §6.1).
func TestQuiesceMethodIsRegisteredAsHandled(t *testing.T) {
	defer resetQuiesceForTest()
	restoreFlush := stubQuiesceFlush()
	defer restoreFlush()

	out, err := handleMethod(pluginabi.MethodPluginQuiesce, nil)
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK {
		t.Fatalf("plugin.quiesce must return an OK envelope, got %s", out)
	}
	if quiesceMethodSupported != pluginabi.MethodPluginQuiesce {
		t.Fatalf("quiesce method constant drifted: %q", quiesceMethodSupported)
	}
	// The method must not be reported as an unknown method.
	if strings.Contains(string(out), "unknown_method") {
		t.Fatalf("plugin.quiesce is not wired into handleMethod: %s", out)
	}
	if !pluginDraining() {
		t.Fatal("draining flag must be set after plugin.quiesce")
	}
}

// TestShutdownExportStaysNoop guards the red line: the cleanup work must live in
// plugin.quiesce, never in cliproxyPluginShutdown (which runs during host
// runtime teardown and caused SIGSEGV when it touched Go state).
func TestShutdownExportStaysNoop(t *testing.T) {
	resetQuiesceForTest()
	restoreFlush := stubQuiesceFlush()
	defer restoreFlush()

	src := readPackageSourceFile(t, "main.go")
	if !strings.Contains(src, "func cliproxyPluginShutdown()") {
		t.Fatal("cliproxyPluginShutdown export is missing")
	}
	shutdownBody := functionBody(t, src, "func cliproxyPluginShutdown()")
	// Match call sites, not substrings that can appear in prose: the existing
	// comment legitimately mentions dlclose().
	for _, forbidden := range []string{
		"stopCheckinScheduler(",
		"stopLoginStatesJanitor(",
		"pluginQuiesce(",
		"close(schedulerStop)",
		"close(loginStatesJanitorStop)",
		"draining.Store(",
	} {
		if strings.Contains(shutdownBody, forbidden) {
			t.Fatalf("cliproxyPluginShutdown must stay a no-op but references %q", forbidden)
		}
	}
}
