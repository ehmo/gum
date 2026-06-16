# GUM module-local make targets.
#
# `make test` runs the standard unit-test suite (fast).
# `make test-matrix` runs the release-gate sweep per docs/test-matrix.md
#   (spawns nested `go test` invocations; slow). Acceptance for gum-b22o.1.
# `make build` produces a local `gum` binary in the module root.

.PHONY: test test-matrix coverage-floor build

test:
	go test ./...

test-matrix:
	go run ./cmd/test-matrix -workdir=. -matrix=../../docs/test-matrix.md

# Per-package line-coverage floor for internal/dispatch and
# internal/output. Floor lives in internal/coverage.FloorPercent (85%).
# Ratchets (per-package current-state minimums) are in
# internal/coverage.Ratchets and each carries a follow-up bead.
coverage-floor:
	go run ./cmd/coverage-floor -workdir=.

build:
	go build -o gum ./cmd/gum
