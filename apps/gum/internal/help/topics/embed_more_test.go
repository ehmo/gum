package topics

import (
	"testing"
)

// TestRead exercises the three Read branches: a present topic returns
// non-empty bytes + true; a missing-but-valid name returns (nil,false);
// an invalid name (traversal / uppercase / wrong rune) short-circuits
// before touching the FS.
func TestRead(t *testing.T) {
	t.Run("present_topic_returns_body", func(t *testing.T) {
		data, ok := Read("gmail")
		if !ok {
			t.Fatal("Read(gmail) ok=false; want true")
		}
		if len(data) == 0 {
			t.Errorf("Read(gmail) returned empty bytes")
		}
	})

	t.Run("missing_topic_returns_false", func(t *testing.T) {
		_, ok := Read("does-not-exist")
		if ok {
			t.Errorf("Read(does-not-exist) ok=true; want false")
		}
	})

	t.Run("invalid_name_returns_false", func(t *testing.T) {
		// Reject path traversal, uppercase, underscore (validTopicName
		// allows only [a-z0-9-]).
		for _, bad := range []string{"", "../etc", "Auth", "topic_name", "topic/slash"} {
			if _, ok := Read(bad); ok {
				t.Errorf("Read(%q) ok=true; want false", bad)
			}
		}
	})
}

// TestValidTopicName covers each branch of the kebab-lowercase guard:
// lowercase letters, digits, and hyphens are accepted; everything else is
// rejected; the empty string is rejected.
func TestValidTopicName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{in: "", want: false},
		{in: "auth", want: true},
		{in: "gmail-readonly", want: true},
		{in: "topic-1", want: true},
		{in: "Auth", want: false},
		{in: "topic_name", want: false},
		{in: "topic/slash", want: false},
		{in: "topic.dot", want: false},
		{in: "topic ", want: false},
		{in: "0", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := validTopicName(tc.in); got != tc.want {
				t.Errorf("validTopicName(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
