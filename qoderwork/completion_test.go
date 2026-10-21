package main

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestCompletionGateAcceptsOncePerRequestID(t *testing.T) {
	gate := newCompletionGate(8, time.Minute)
	now := time.Now()
	if !gate.accept("req-1", now) {
		t.Fatal("first accept must be true")
	}
	if gate.accept("req-1", now) {
		t.Fatal("second accept for the same RequestID must be false")
	}
	if !gate.accept("req-2", now) {
		t.Fatal("a different RequestID must be accepted")
	}
}

func TestCompletionGateRejectsEmptyKeyWithoutRecording(t *testing.T) {
	gate := newCompletionGate(8, time.Minute)
	now := time.Now()
	for _, key := range []string{"", "   ", "\t"} {
		if gate.accept(key, now) {
			t.Fatalf("blank key %q must not be accepted", key)
		}
	}
	if gate.len() != 0 {
		t.Fatalf("gate recorded %d entries for blank keys, want 0", gate.len())
	}
}

// TestCompletionGateIsSingleFlight asserts the concurrency contract: N goroutines
// racing on one RequestID yield exactly one acceptance.
func TestCompletionGateIsSingleFlight(t *testing.T) {
	gate := newCompletionGate(64, time.Minute)
	const racers = 64
	var accepted int64
	var mu sync.Mutex
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if gate.accept("shared-request", time.Now()) {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted != 1 {
		t.Fatalf("accepted = %d, want exactly 1", accepted)
	}
}

func TestCompletionGateEvictsByCapacity(t *testing.T) {
	gate := newCompletionGate(4, time.Minute)
	base := time.Now()
	for i := 0; i < 10; i++ {
		gate.accept("req-"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Millisecond))
	}
	if gate.len() > 4 {
		t.Fatalf("gate holds %d entries, want <= 4 (bounded memory)", gate.len())
	}
}

func TestCompletionGateExpiresByTTL(t *testing.T) {
	gate := newCompletionGate(16, time.Minute)
	start := time.Now()
	if !gate.accept("req-1", start) {
		t.Fatal("first accept must be true")
	}
	if gate.accept("req-1", start.Add(30*time.Second)) {
		t.Fatal("within TTL the RequestID must stay deduped")
	}
	if !gate.accept("req-1", start.Add(2*time.Minute)) {
		t.Fatal("after TTL the RequestID must be re-admitted")
	}
}

func TestRequestCompleteRecordsCanceledOnce(t *testing.T) {
	resetCompletionStateForTest()
	if recorded, payload := runRequestComplete(t, "req-canceled", pluginapi.RequestCompletionCanceled); !recorded {
		t.Fatalf("canceled completion must be recorded, payload=%s", payload)
	}
	if recorded, payload := runRequestComplete(t, "req-canceled", pluginapi.RequestCompletionCanceled); recorded {
		t.Fatalf("replayed RequestID must not be recorded again, payload=%s", payload)
	}
}

func TestRequestCompleteIgnoresObservedOutcomes(t *testing.T) {
	resetCompletionStateForTest()
	for _, outcome := range []pluginapi.RequestCompletionOutcome{
		pluginapi.RequestCompletionSucceeded,
		pluginapi.RequestCompletionFailed,
		pluginapi.RequestCompletionRejected,
	} {
		if recorded, payload := runRequestComplete(t, "req-"+string(outcome), outcome); recorded {
			t.Fatalf("outcome %q is already accounted by the executor path; payload=%s", outcome, payload)
		}
	}
}

func TestRequestCompleteIgnoresEmptyRequestID(t *testing.T) {
	resetCompletionStateForTest()
	if recorded, _ := runRequestComplete(t, "   ", pluginapi.RequestCompletionCanceled); recorded {
		t.Fatal("a blank RequestID is not a usable idempotency key")
	}
}

// runRequestComplete drives the request.complete RPC and reports whether the
// plugin recorded the completion.
func runRequestComplete(t *testing.T, requestID string, outcome pluginapi.RequestCompletionOutcome) (bool, string) {
	t.Helper()
	raw := mustJSON(map[string]any{
		"RequestID":      requestID,
		"TraceID":        "trace-1",
		"SourceFormat":   "chat-completions",
		"Model":          "upstream-model",
		"RequestedModel": "client-model",
		"Stream":         true,
		"Outcome":        string(outcome),
		"StartedAt":      time.Now().Add(-time.Second).Format(time.RFC3339),
		"Metadata":       map[string]any{"canonical_session_id": "sess-1"},
	})
	out, err := handleMethod(pluginabi.MethodRequestComplete, raw)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Recorded bool `json:"recorded"`
	}
	decodeEnvelopeResult(t, out, &result)
	return result.Recorded, string(out)
}

// resetCompletionStateForTest gives each test a fresh gate.
func resetCompletionStateForTest() {
	completionGateInstance = newCompletionGate(completionGateSize, completionGateTTL)
}

// TestRequestCompleteNeverReadsMetadataKeys pins the dedupe contract (A1-23): the
// gate keys on RequestID only.
func TestRequestCompleteNeverReadsMetadataKeys(t *testing.T) {
	resetCompletionStateForTest()

	send := func(requestID string, metadata map[string]any) bool {
		t.Helper()
		raw := mustJSON(map[string]any{
			"RequestID": requestID,
			"Outcome":   string(pluginapi.RequestCompletionCanceled),
			"Metadata":  metadata,
		})
		out, err := handleMethod(pluginabi.MethodRequestComplete, raw)
		if err != nil {
			t.Fatal(err)
		}
		var env envelope
		if err := json.Unmarshal(out, &env); err != nil {
			t.Fatal(err)
		}
		if !env.OK {
			t.Fatalf("request.complete must succeed: %s", out)
		}
		var result struct {
			Recorded bool `json:"recorded"`
		}
		if err := json.Unmarshal(env.Result, &result); err != nil {
			t.Fatal(err)
		}
		return result.Recorded
	}

	lookalike := map[string]any{"request_id": "shared-lookalike", "id": "shared-lookalike"}
	if !send("req-a", lookalike) {
		t.Fatal("req-a must be recorded: its RequestID is new")
	}
	if !send("req-b", lookalike) {
		t.Fatal("req-b must be recorded: the gate keys on RequestID, not on Metadata values")
	}
	if !send("req-c", map[string]any{"canonical_session_id": "sess-1"}) {
		t.Fatal("req-c must be recorded on first sight")
	}
	if send("req-c", map[string]any{"canonical_session_id": "sess-2", "id": "req-c"}) {
		t.Fatal("req-c must not be recorded twice just because Metadata differs")
	}
}
