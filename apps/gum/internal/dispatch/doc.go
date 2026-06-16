// Package dispatch owns the 9-step invocation lifecycle and policy kernel (spec.md §3.1, §14).
//
// parse → policy → routing → cache → auth → token bucket → executor → shape → return.
// Must not depend on internal/cli or internal/mcp. Must not import CGo.
package dispatch
