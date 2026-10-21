package main

// Test-only helpers shared across this package's test files.

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// resetAccountCacheForTest clears the per-account plan/checkin/credits cache and
// any in-flight singleflight entries, so a test that exercises a cached path
// starts from a known-empty state instead of a neighbour test's snapshot.
func resetAccountCacheForTest() {
	accountCache.Range(func(key, _ any) bool {
		accountCache.Delete(key)
		return true
	})
	accountDetailFlight.Range(func(key, _ any) bool {
		accountDetailFlight.Delete(key)
		return true
	})
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// stubUpstreamTransport points the plugin's shared HTTP client at a fake
// transport so upstream calls (host bridge unavailable in unit tests) hit a
// deterministic in-process handler instead of the network. This keeps the real
// fetch/cache code under test without touching upstreamBaseCN (a const baked into
// every endpoint literal).
func stubUpstreamTransport(t *testing.T, fn roundTripFunc) {
	t.Helper()
	// Mark the lazy initialiser as done without letting it install its own
	// transport, then swap the client.
	httpClientOnce.Do(func() {})
	prev := sharedClient
	sharedClient = &http.Client{Transport: fn}
	t.Cleanup(func() { sharedClient = prev })
}

// readPackageSourceFile reads a source file from this package's directory.
func readPackageSourceFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

// functionBody extracts the body of the named function declaration using brace
// matching, so structural assertions do not depend on formatting.
func functionBody(t *testing.T, src, signature string) string {
	t.Helper()
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("signature %q not found", signature)
	}
	open := strings.Index(src[start:], "{")
	if open < 0 {
		t.Fatalf("no body for %q", signature)
	}
	open += start
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	t.Fatalf("unbalanced braces for %q", signature)
	return ""
}

// resetOnceForTest is a tiny helper used where a sync.Once must be re-armed.
func resetOnceForTest(once *sync.Once) { *once = sync.Once{} }
