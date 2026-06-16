package main

import (
	"strings"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// TestTokenCountMarshalErrorWrapped pins the json.Marshal error branch:
// a channel value is not serialisable, so tokenCount must surface a
// wrapped "marshal:" error instead of returning (0, nil) — a regression
// here would silently hide measurement failures behind a zero count.
func TestTokenCountMarshalErrorWrapped(t *testing.T) {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Skipf("tokenizer unavailable: %v", err)
	}
	n, err := tokenCount(enc, make(chan int))
	if err == nil {
		t.Fatal("want marshal error; got nil")
	}
	if n != 0 {
		t.Errorf("count=%d; want 0 on error", n)
	}
	if !strings.HasPrefix(err.Error(), "marshal:") {
		t.Errorf("err=%v; want 'marshal:' prefix", err)
	}
}
