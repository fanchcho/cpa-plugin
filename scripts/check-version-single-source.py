#!/usr/bin/env python3
"""Fail if a plugin version is not single-sourced from its VERSION file.

Single source of truth (DESIGN §12 P1): ``<plugin>/VERSION``.
The code default (``var version = "..."``) is only a placeholder for
non-release local builds; the release build injects the value via
``-ldflags "-X main.version=$(cat VERSION)"``.

Checks:
  1. ``<plugin>/main.go`` default literal == ``<plugin>/VERSION``
  2. ``registry.json`` entry ``version`` == ``<plugin>/VERSION``
  3. READMEs (root + per-plugin) carry NO concrete plugin-version literal:
     no bare ``<plugin>_<semver>_<goos>_<goarch>`` archive name, no
     ``-X main.version=<semver>``, and no literal equal to VERSION itself.

CHANGELOG files and historical ``docs/`` records are exempt: they must
name past versions by design.
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PLUGINS = ("workbuddy", "qoderwork")

# ``workbuddy_0.9.3_linux_amd64.zip`` with a *literal* semver in the middle.
ARCHIVE_LITERAL_RE = re.compile(
    r"(?P<id>[A-Za-z0-9][A-Za-z0-9._-]*)_(?P<ver>[0-9]+\.[0-9]+\.[0-9]+)_"
    r"(?P<tail>[A-Za-z0-9._-]+)"
)
LDFLAGS_LITERAL_RE = re.compile(r"-X\s+main\.version=(?P<ver>[0-9][0-9A-Za-z.+-]*)")

# READMEs that describe install/release and therefore must stay version-free.
README_CANDIDATES = (
    ("repo", Path("README.md")),
    ("workbuddy", Path("workbuddy/README.md")),
    ("workbuddy", Path("workbuddy/README_CN.md")),
    ("qoderwork", Path("qoderwork/README.md")),
    ("qoderwork", Path("qoderwork/README_CN.md")),
)


def read_version(plugin: str) -> str:
    raw = (ROOT / plugin / "VERSION").read_text(encoding="utf-8")
    return raw.strip()


def code_default(plugin: str) -> str:
    src = (ROOT / plugin / "main.go").read_text(encoding="utf-8")
    match = re.search(r'^var version = "([^"]*)"', src, re.MULTILINE)
    if not match:
        raise SystemExit(f"{plugin}/main.go: no `var version = \"...\"` declaration found")
    return match.group(1)


def registry_versions() -> dict[str, str]:
    data = json.loads((ROOT / "registry.json").read_text(encoding="utf-8"))
    out: dict[str, str] = {}
    for entry in data.get("plugins") or []:
        out[str(entry.get("id", "")).strip()] = str(entry.get("version") or "").strip()
    return out


def main() -> int:
    versions = {p: read_version(p) for p in PLUGINS}
    reg = registry_versions()
    failures: list[str] = []

    for plugin, want in versions.items():
        got = code_default(plugin)
        if got != want:
            failures.append(
                f"{plugin}/main.go: default version {got!r} != VERSION {want!r}"
            )
        if plugin not in reg:
            failures.append(f"registry.json: no entry for plugin {plugin!r}")
        elif reg[plugin] != want:
            failures.append(
                f"registry.json: {plugin} version {reg[plugin]!r} != VERSION {want!r}"
            )

    for owner, rel in README_CANDIDATES:
        path = ROOT / rel
        if not path.is_file():
            continue
        text = path.read_text(encoding="utf-8")
        for lineno, line in enumerate(text.splitlines(), 1):
            archive = ARCHIVE_LITERAL_RE.search(line)
            if archive and archive.group("id") in versions:
                failures.append(
                    f"{rel}:{lineno}: literal archive version "
                    f"{archive.group('ver')!r} for {archive.group('id')!r} — use <version> placeholder"
                )
            ldflags = LDFLAGS_LITERAL_RE.search(line)
            if ldflags:
                failures.append(
                    f"{rel}:{lineno}: literal -X main.version="
                    f"{ldflags.group('ver')!r} — read VERSION instead"
                )
            for plugin, lit in versions.items():
                if re.search(rf"(?<![0-9A-Za-z.+-]){re.escape(lit)}(?![0-9A-Za-z.+-])", line):
                    failures.append(
                        f"{rel}:{lineno}: literal plugin version {lit!r} ({plugin})"
                    )

    if failures:
        print("version single-source check: FAILED", file=sys.stderr)
        for item in failures:
            print(f"  - {item}", file=sys.stderr)
        return 1

    joined = ", ".join(f"{p}={v}" for p, v in sorted(versions.items()))
    print(f"OK version single-source: {joined} (VERSION == code default == registry; READMEs clean)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())