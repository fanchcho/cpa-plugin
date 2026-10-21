package main

// Test-only helpers shared across this package's test files.

import (
	"os"
	"strings"
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

// readPackageSourceFile reads a source file from this package's directory.
func readPackageSourceFile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

// functionBody extracts the body of the named function declaration using
// brace matching, so structural assertions do not depend on formatting.
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
