# GUM module-local make targets.
#
# `make test` runs the standard unit-test suite (fast).
# `make test-matrix` runs the release-gate sweep per docs/test-matrix.md
#   (spawns nested `go test` invocations; slow). Acceptance for gum-b22o.1.
# `make build` produces a local `gum` binary in the module root.

.PHONY: test test-matrix coverage-floor build lint fmt-check

test:
	go test ./...

# gofmt drift is invisible to vet, staticcheck and golangci-lint's default
# set, so it accumulated to 71 files before this gate existed (gum-ln4c).
# gen/ is generated and gitignored, so it is excluded.
fmt-check:
	@unformatted=$$(gofmt -l ./cmd ./internal); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt drift (run: gofmt -w ./cmd ./internal):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

# Same linter set and version the `test` workflow runs. Requires
# golangci-lint v2.12.1 on PATH.
lint:
	golangci-lint run ./...

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
