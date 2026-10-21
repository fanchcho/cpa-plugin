#!/usr/bin/env bash
# Run a Go command inside the pinned golang toolchain container.
#
# P-ENV workaround: this host has no local Go toolchain (DESIGN §10.2), so the
# build/test/verify steps run in a container that shares the repo tree and a
# persistent module cache. CI remains the authoritative gate (PRD AC-1); this
# script exists so the same commands can be exercised locally.
#
# Usage: scripts/go-docker.sh <plugin-dir> <go args...>
#   scripts/go-docker.sh workbuddy test ./...
#   scripts/go-docker.sh qoderwork vet ./...
set -euo pipefail

PLUGIN="${1:?usage: go-docker.sh <plugin-dir> <go args...>}"
shift
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${GO_IMAGE:-golang:1.26}"

exec docker run --rm \
  -v "${ROOT}":/w \
  -v cpa-plugin-go-mod-cache:/go/pkg/mod \
  -v cpa-plugin-go-build-cache:/root/.cache/go-build \
  -w "/w/${PLUGIN}" \
  "${IMAGE}" go "$@"
