package mcp

// registerHelpResources runs topics.ValidateSizes as a build-time fail-safe
// and panics on HELP_TOPIC_TOO_LARGE, so an oversized help body stops the
// server before a client can read a truncated topic. Every NewServer call
// exercises the success path; nothing reached the panic, because the shipped
// topics all fit and the embedded filesystem cannot be swapped from this
// package. validateHelpTopicSizes is the seam that makes the arm reachable
// (bead gum-p1ko).

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/help/topics"
)

func TestRegisterHelpResourcesPanicsOnOversizedTopic(t *testing.T) {
	prev := validateHelpTopicSizes
	validateHelpTopicSizes = func() error {
		return &topics.ErrTopicTooLarge{Topic: "gmail", Size: topics.MaxTopicBytes + 1}
	}
	t.Cleanup(func() { validateHelpTopicSizes = prev })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registerHelpResources returned normally with an oversized topic; it must panic so the server never serves a truncated body")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value %T = %v; want a string", r, r)
		}
		if !strings.Contains(msg, "HELP_TOPIC_TOO_LARGE") {
			t.Errorf("panic message %q; want the HELP_TOPIC_TOO_LARGE code so the operator can act on it", msg)
		}
	}()

	makeHelpServer().registerHelpResources()
}

// TestValidateHelpTopicSizesIsTheRealCheck keeps the seam honest: the default
// value must be topics.ValidateSizes, and the shipped topics must pass it.
func TestValidateHelpTopicSizesIsTheRealCheck(t *testing.T) {
	if err := validateHelpTopicSizes(); err != nil {
		t.Fatalf("validateHelpTopicSizes on the shipped topics: %v", err)
	}
	var tooLarge *topics.ErrTopicTooLarge
	if errors.As(topics.ValidateSizes(), &tooLarge) {
		t.Fatalf("topics.ValidateSizes reports %v; the seam and the real check disagree", tooLarge)
	}
}
