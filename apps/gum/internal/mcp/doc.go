// Package mcp is the thin MCP server presentation layer (spec.md §14).
//
// Registers meta-tools and convenience tools against a dispatch.Dispatcher.
// Presentation layers stay thin; internal/dispatch owns the invocation lifecycle.
package mcp
