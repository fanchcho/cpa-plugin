// quiesce.go implements the plugin.quiesce contract for QoderWork (DESIGN §6).
//
// The host calls plugin.quiesce when it detects that this plugin's file was
// replaced, immediately BEFORE loading the new build
// (internal/pluginhost/host.go). Everything that must stop belongs here:
// background timers, and the draining flag that makes the outgoing instance fail
// fast instead of serving requests while its successor boots.
//
// RED LINE: none of this may move into cliproxyPluginShutdown. That export is
// deliberately a no-op because the host calls it during its own runtime teardown
// and dlclose()es the library right afterwards — touching Go runtime state there
// caused SIGSEGV on every restart. quiesce runs while the plugin is still fully
// alive, which is exactly why the host added it.
package main

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

// draining marks this plugin instance as shutting down (hot reload in flight).
// Set only by pluginQuiesce; never cleared — the instance is disposable.
var draining atomic.Bool

// pluginDraining reports whether plugin.quiesce already ran.
func pluginDraining() bool { return draining.Load() }

var (
	quiesceOnce  sync.Once
	quiesceFlush func() []string
)

// quiesceModelFlush is the state-flush hook. QoderWork has NO on-disk state cache
// (no model store, no metadata cache — verified by grep for writeModelCacheAtomic
// / saveMetadata: zero hits), so its flush obligation is the EMPTY SET
// (DESIGN §6.3). The hook exists so the contract is testable and so a future
// persisted state has one obvious place to land; it must not invent files now.
var quiesceModelFlush = func() error {
	return nil
}

// pluginQuiesce performs the handover: stop timers, land state, refuse new work.
// It is idempotent — a repeated call is a no-op rather than a double close.
func pluginQuiesce() {
	quiesceOnce.Do(func() {
		draining.Store(true)
		stopCheckinScheduler()
		stopLoginStatesJanitor()
		quiesceFlush = flushQuiesceState
		flushQuiesceState()
	})
}

// resetQuiesceForTest clears the one-shot quiesce state so a test can exercise
// the draining path without leaving the package permanently draining.
// Production code never calls it.
func resetQuiesceForTest() {
	quiesceOnce = sync.Once{}
	quiesceFlush = nil
	draining.Store(false)
}

// lastQuiesceFlush exposes the flush notes for tests/diagnostics. Nil until
// quiesce ran.
func lastQuiesceFlush() []string {
	if quiesceFlush == nil {
		return nil
	}
	return quiesceFlush()
}

// handlePluginQuiesce is the RPC entry point. The host only inspects ok/err, so
// the result body is empty.
func handlePluginQuiesce() ([]byte, error) {
	pluginQuiesce()
	return okEnvelope(struct{}{})
}

// flushQuiesceState re-persists process-internal state that already has an
// on-disk home. For QoderWork that set is empty (see quiesceModelFlush); the
// function still reports failures rather than swallowing them, so a future
// persisted state cannot fail silently.
func flushQuiesceState() []string {
	var notes []string
	if err := quiesceModelFlush(); err != nil {
		notes = append(notes, "state: "+err.Error())
	}
	return notes
}

// drainErrorBody builds the envelope the host turns into a downstream 503.
//
// The message is itself a JSON object on purpose: the SDK's
// buildErrorResponseBody returns a JSON-valid error message verbatim, which is
// the only way the literal "retryable":true reaches the client — the host's
// rpcError keeps Code/message/statusCode and drops the plugin's Retryable flag
// (DESIGN §6.4).
func drainErrorBody() []byte {
	inner := map[string]any{
		"error": map[string]any{
			"message":   "plugin is draining after hot-reload",
			"type":      "server_error",
			"code":      "plugin_unavailable",
			"retryable": true,
		},
	}
	raw, err := json.Marshal(inner)
	if err != nil {
		// Unreachable: the map above is always marshalable.
		raw = []byte(`{"error":{"message":"plugin is draining after hot-reload","type":"server_error","code":"plugin_unavailable","retryable":true}}`)
	}
	return raw
}

// errorEnvelopeDraining is the fast-fail response for new work after quiesce.
func errorEnvelopeDraining() []byte {
	return errorEnvelopeWithRetryable(
		"plugin_draining",
		string(drainErrorBody()),
		503,
		true,
	)
}

// guardDraining returns the fast-fail envelope when the instance is draining.
func guardDraining() []byte {
	if !pluginDraining() {
		return nil
	}
	return errorEnvelopeDraining()
}

// quiesceMethodSupported names the RPC method this plugin implements. Kept as a
// package-level constant so tests and diagnostics reference one definition.
const quiesceMethodSupported = pluginabi.MethodPluginQuiesce
