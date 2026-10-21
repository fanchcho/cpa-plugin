package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultVersionMatchesVersionFile pins the plugin version single source of
// truth (DESIGN §12 P1, PRD AC-7): VERSION is authoritative, the in-code
// default is only a placeholder for non-release builds and must not drift.
func TestDefaultVersionMatchesVersionFile(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(string(raw)); version != want {
		t.Fatalf("default version %q does not match VERSION %q", version, want)
	}
}

// versionFileDir resolves the directory holding VERSION. It stays valid after
// the standalone-repo split (DESIGN §8.1), where the plugin content is moved
// from <repo>/qoderwork/ to the repository root and the test's working
// directory becomes the repository root.
func versionFileDir(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("VERSION"); err == nil {
		return "."
	}
	t.Fatal("VERSION file not found in the working directory")
	return ""
}

// registryCandidates lists every layout in which registry.json may sit
// relative to the current plugin test working directory: monorepo
// (<repo>/qoderwork/ -> <repo>/registry.json) and standalone repo
// (repository root -> ./registry.json).
func registryCandidates() []string {
	return []string{
		filepath.Join("..", "registry.json"),
		"registry.json",
	}
}

func loadRegistry(t *testing.T) (map[string]any, string) {
	t.Helper()
	for _, candidate := range registryCandidates() {
		raw, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: invalid JSON: %v", candidate, err)
		}
		return doc, candidate
	}
	t.Skip("registry.json not present (standalone layout without registry)")
	return nil, ""
}

// TestRegistryVersionMatchesVersionFile guards registry drift. A registry
// version that disagrees with VERSION makes the store advertise a different
// version than the release tag resolves to (PRD AC-7 / D-C).
func TestRegistryVersionMatchesVersionFile(t *testing.T) {
	dir := versionFileDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(raw))

	doc, path := loadRegistry(t)
	plugins, ok := doc["plugins"].([]any)
	if !ok {
		t.Fatalf("%s: plugins is not an array", path)
	}
	found := false
	for _, entry := range plugins {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := item["id"].(string); strings.TrimSpace(id) != providerName {
			continue
		}
		found = true
		got, _ := item["version"].(string)
		if strings.TrimSpace(got) != want {
			t.Fatalf("%s: plugin %q version %q does not match VERSION %q", path, providerName, got, want)
		}
	}
	if !found {
		t.Fatalf("%s: no entry for plugin %q", path, providerName)
	}
}

// containsLiteralVersion reports whether line contains version as a standalone
// token. Go's regexp (RE2) has no lookaround, so the boundary test is done by
// hand: a neighbouring byte in the version alphabet means it is part of a
// larger token (e.g. "10.4.10") rather than a standalone literal.
func containsLiteralVersion(line, version string) bool {
	if version == "" {
		return false
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz.+-"
	boundary := func(b byte) bool { return strings.IndexByte(alphabet, b) >= 0 }

	for offset := 0; ; {
		idx := strings.Index(line[offset:], version)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(version)
		leftOK := start == 0 || !boundary(line[start-1])
		rightOK := end == len(line) || !boundary(line[end])
		if leftOK && rightOK {
			return true
		}
		offset = start + 1
		if offset >= len(line) {
			return false
		}
	}
}

// TestReadmeHasNoVersionLiteral keeps install docs free of version drift: they
// must reference the placeholder/VERSION rather than a frozen number.
func TestReadmeHasNoVersionLiteral(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(raw))

	for _, name := range []string{"README.md", "README_CN.md"} {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if containsLiteralVersion(line, want) {
				t.Errorf("%s:%d: README embeds the version literal %q; read VERSION instead: %s", name, i+1, want, strings.TrimSpace(line))
			}
		}
	}
}