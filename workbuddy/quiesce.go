// quiesce.go implements the plugin.quiesce contract (DESIGN §6).
//
// The host calls plugin.quiesce when it detects that this plugin's file was
// replaced, immediately BEFORE loading the new build
// (internal/pluginhost/host.go). Everything that must stop belongs here:
// background timers, and the draining flag that makes the outgoing instance
// fail fast instead of serving requests while its successor boots.
//
// RED LINE: none of this may move into cliproxyPluginShutdown. That export is
// deliberately a no-op because the host calls it during its own runtime
// teardown and dlclose()es the library right afterwards — touching Go runtime
// state there caused SIGSEGV on every restart. quiesce runs while the plugin is
// still fully alive, which is exactly why the host added it.
package main

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

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

// quiesceModelFlush is the state-flush hook. It is a variable so tests can
// substitute a fake store instead of writing into the developer's real config
// directory.
var quiesceModelFlush = func() error {
	return currentModelRuntime().flushMetadataCache()
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
// the draining path without leaving the package permanently draining (other
// tests in this package drive handleManagement/handleExecExecute directly).
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
// on-disk home, so a hot reload cannot lose it. The write goes through the
// existing atomic helper (temp file in the same directory + fsync + rename), so
// a crash mid-flush can never leave a half-written cache file behind.
//
// Scope is deliberately limited to state that ALREADY has a persistence path
// (DESIGN §6.3). Keepalive summaries and login-state maps are volatile
// observability data with no on-disk representation; inventing files for them
// at quiesce time would be new state, not a flush.
func flushQuiesceState() []string {
	var notes []string
	if err := quiesceModelFlush(); err != nil {
		notes = append(notes, "model-catalog: "+err.Error())
	}
	return notes
}

// flushMetadataCache writes the in-memory models.dev metadata cache back to
// disk atomically. It is a no-op when nothing has been cached yet (fresh
// process, or a run that never fetched metadata).
func (r *modelRuntime) flushMetadataCache() error {
	if r == nil || r.store == nil {
		return nil
	}
	r.metadataMu.Lock()
	cache := r.metadataCache
	r.metadataMu.Unlock()
	if cache == nil {
		return nil
	}
	fresh := *cache
	fresh.FetchedAt = time.Now().UTC()
	return r.store.saveMetadata(fresh)
}

// drainErrorBody builds the envelope the host turns into a downstream 503.
//
// The message is itself a JSON object on purpose:
// sdk/api/handlers/buildErrorResponseBody returns a JSON-valid error message
// verbatim, which is the only way the literal "retryable":true reaches the
// client — the host's rpcError keeps Code/message/statusCode and drops the
// plugin's Retryable flag (DESIGN §6.4).
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
