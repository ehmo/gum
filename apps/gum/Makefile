# GUM module-local make targets.
#
# `make test` runs the standard unit-test suite (fast).
# `make test-matrix` runs the release-gate sweep per docs/test-matrix.md
#   (spawns nested `go test` invocations; slow). Acceptance for gum-b22o.1.
# `make build` produces a local `gum` binary in the module root.

.PHONY: test test-matrix coverage-floor build lint fmt-check stubs

test:
	go test ./...

# Regenerates gen/dispatch/stub_*.go from the embedded catalog. Offline: it
# reads internal/embedded/catalog.json and neither fetches Discovery nor
# rewrites the catalog. It sweeps stub_*.go only, leaving the handwritten
# dispatch.go and its tests alone.
#
# Use this instead of `go generate ./gen/dispatch`. The whole gen/ tree is
# gitignored, so on a fresh checkout the directory — and the //go:generate
# directive inside dispatch.go — does not exist yet, and go generate fails with
# "lstat ./gen/dispatch/: no such file or directory".
stubs:
	go run ./cmd/gen-catalog -offline-stubs-only -out=internal/embedded/catalog.json -stubs-out=gen/dispatch

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

# Same linter set and versions the `test` workflow runs. Requires
# golangci-lint v2.12.1 on PATH; staticcheck is pinned here and fetched on
# demand. Both are needed and neither subsumes the other: golangci-lint
# bundles errcheck, and bare staticcheck honours only //lint: directives
# while golangci-lint honours only //nolint. Running just one of them let a
# 26-finding SA1019 batch reach CI (gum-8i2f).
# The install clears GOOS/GOARCH so the tool is always built for the host;
# the run keeps them, so `GOOS=linux make lint` analyzes the linux build
# with a binary that can actually execute.
lint:
	golangci-lint run ./...
	@GOOS= GOARCH= go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
	@"$$(GOOS= GOARCH= go env GOPATH)"/bin/staticcheck ./...

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
