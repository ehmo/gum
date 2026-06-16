// Package topics embeds the per-topic markdown bodies served by the
// gum://help/{topic} MCP resource template. The eight v0.1.0 active topics
// (spec §13 line 3150) are validated against an 8 KiB ceiling at process
// startup; anything larger fails the build with HELP_TOPIC_TOO_LARGE.
package topics

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed *.md
var topicsFS embed.FS

// MaxTopicBytes is the spec §13 line 3159 ceiling on rendered help body
// length. Active topics exceeding this fail the build with
// HELP_TOPIC_TOO_LARGE (see ErrTopicTooLarge).
const MaxTopicBytes = 8 * 1024

// ErrTopicTooLarge is the spec §11 sentinel surfaced at build time when a
// topic body exceeds MaxTopicBytes. Carries the topic name and observed
// size for clearer remediation messages.
type ErrTopicTooLarge struct {
	Topic string
	Size  int
}

func (e *ErrTopicTooLarge) Error() string {
	return fmt.Sprintf("HELP_TOPIC_TOO_LARGE: topic %q is %d bytes (max %d)",
		e.Topic, e.Size, MaxTopicBytes)
}

// Read returns the markdown body for the named topic. Returns (nil, false)
// if no embedded file exists for that name (callers map this to
// RESOURCE_NOT_FOUND).
func Read(name string) ([]byte, bool) {
	if !validTopicName(name) {
		return nil, false
	}
	data, err := topicsFS.ReadFile(name + ".md")
	if err != nil {
		return nil, false
	}
	return data, true
}

// ValidateSizes walks every embedded topic and returns the first
// ErrTopicTooLarge it finds. Returns nil when every body fits in
// MaxTopicBytes. Called from process startup and from the build-time test
// so a regression is caught long before clients see truncated help.
func ValidateSizes() error {
	entries, err := topicsFS.ReadDir(".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := topicsFS.ReadFile(name)
		if err != nil {
			return err
		}
		if len(data) > MaxTopicBytes {
			return &ErrTopicTooLarge{
				Topic: strings.TrimSuffix(name, ".md"),
				Size:  len(data),
			}
		}
	}
	return nil
}

// Names returns the sorted list of topic identifiers that have an
// embedded markdown body. Used by tests to cross-check
// docs/help-topics.v1.json against the embedded files.
func Names() []string {
	entries, err := topicsFS.ReadDir(".")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".md"))
	}
	return names
}

// validTopicName guards against directory traversal and stray separators
// in the URI parameter. Topics are kebab-lowercase only.
func validTopicName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}
