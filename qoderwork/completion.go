// completion.go implements the request_lifecycle_plugin capability
// (HandleRequestComplete) plus the RequestID idempotency gate (DESIGN §5) for
// QoderWork. Ported from the workbuddy contract (D7: workbuddy freezes, qoderwork
// follows) so both plugins dedupe on exactly the same key and bounds.
package main

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	// completionGateSize bounds the dedupe table. The host calls this for every
	// request that enters the execution chain, so an unbounded map would be a
	// memory leak (DESIGN §5.2 item 3).
	completionGateSize = 4096
	// completionGateTTL bounds how long a RequestID stays deduped.
	completionGateTTL = 30 * time.Minute
)

// completionGate is a bounded, TTL'd set of RequestIDs. Its only job is to make
// HandleRequestComplete idempotent on RequestID (PRD 5.3 / AC-8).
type completionGate struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	order []string
	size  int
	ttl   time.Duration
}

func newCompletionGate(size int, ttl time.Duration) *completionGate {
	if size <= 0 {
		size = completionGateSize
	}
	if ttl <= 0 {
		ttl = completionGateTTL
	}
	return &completionGate{
		seen: make(map[string]time.Time, size),
		size: size,
		ttl:  ttl,
	}
}

// accept reports whether requestID is being seen for the first time. The lookup
// and the insert happen under one lock, so N concurrent calls with the same
// RequestID yield exactly one true (DESIGN §5.2 item 1).
//
// An empty (or whitespace-only) RequestID is not a usable idempotency key: it
// returns false without recording anything — we would rather under-report once
// than risk double-reporting a shared empty key (DESIGN §5.2 item 2).
func (g *completionGate) accept(requestID string, now time.Time) bool {
	key := strings.TrimSpace(requestID)
	if g == nil || key == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen == nil {
		g.seen = make(map[string]time.Time, g.size)
	}
	if at, ok := g.seen[key]; ok {
		if now.Sub(at) <= g.ttl {
			return false
		}
		// Expired entry: re-admit under the same key so a recycled RequestID is
		// not silently dropped forever.
		delete(g.seen, key)
		g.removeFromOrder(key)
	}
	g.seen[key] = now
	g.order = append(g.order, key)
	g.evictLocked(now)
	return true
}

// evictLocked drops expired entries and trims the FIFO order to size.
func (g *completionGate) evictLocked(now time.Time) {
	kept := g.order[:0]
	for _, key := range g.order {
		at, ok := g.seen[key]
		if !ok {
			continue
		}
		if now.Sub(at) > g.ttl {
			delete(g.seen, key)
			continue
		}
		kept = append(kept, key)
	}
	g.order = kept
	for len(g.order) > g.size {
		oldest := g.order[0]
		g.order = g.order[1:]
		delete(g.seen, oldest)
	}
}

func (g *completionGate) removeFromOrder(key string) {
	for i, existing := range g.order {
		if existing == key {
			g.order = append(g.order[:i], g.order[i+1:]...)
			return
		}
	}
}

func (g *completionGate) len() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.seen)
}

// completionGateInstance is this plugin's gate. One per plugin instance; it is
// intentionally not shared with workbuddy's build.
var completionGateInstance = newCompletionGate(completionGateSize, completionGateTTL)

// requestCompletionWire mirrors the host's rpcRequestCompletion (the pluginapi
// RequestCompletion plus the host callback id).
type requestCompletionWire struct {
	pluginapi.RequestCompletion
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

// handleRequestComplete is the RequestLifecyclePlugin entry point. The host
// invokes it asynchronously on a context detached from the request
// (context.WithoutCancel in CompleteRequestExcept), so this function must not
// assume the request context is alive and must return promptly.
//
// Dedupe rule: RequestID is the only idempotency key. Metadata key names are
// host-internal and are never matched here (PRD R3 / DESIGN §5.2 item 4).
func handleRequestComplete(raw []byte) ([]byte, error) {
	var req requestCompletionWire
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if !completionGateInstance.accept(req.RequestID, time.Now()) {
		return okEnvelope(map[string]any{"recorded": false})
	}
	if !isUnobservedOutcome(req.Outcome) {
		// The executor + UsagePlugin paths already account for this outcome;
		// recording it again here would double-report usage (PRD 5.3).
		return okEnvelope(map[string]any{"recorded": false})
	}
	recordCanceledCompletion(req)
	return okEnvelope(map[string]any{"recorded": true})
}

// isUnobservedOutcome reports whether the terminal outcome is one the plugin's
// own executor path cannot observe. A downstream client disconnect surfaces only
// as RequestCompletionCanceled (sdk/api/handlers/handlers_interceptors.go), which
// is precisely the gap AC-8 asks us to close.
func isUnobservedOutcome(outcome pluginapi.RequestCompletionOutcome) bool {
	return outcome == pluginapi.RequestCompletionCanceled
}

// recordCanceledCompletion forwards the canceled terminal state to the same usage
// sink the executor uses. Zero token detail is correct: a canceled request has no
// completed usage counters to report. Status 499 mirrors the non-standard "client
// closed request" convention.
func recordCanceledCompletion(req requestCompletionWire) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(req.RequestedModel)
	}
	publishUsage(req.RequestedModel, model, "", req.StartedAt, usage.Detail{}, true, 499, "canceled: downstream disconnect or context cancellation")
}
