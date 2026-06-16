package toon_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestDecodeTypedDocumentPropagatesDocumentDecodeError pins
// DecodeTypedDocument's `DecodeTOONDocument err → return nil, err` arm
// (typed.go:77-79). When the input bytes aren't even a valid TOON
// document, the typed decoder MUST surface that err verbatim rather
// than continue past the nil document and trip on toon.Fields nil
// dereferences downstream.
func TestDecodeTypedDocumentPropagatesDocumentDecodeError(t *testing.T) {
	t.Parallel()
	// Not a TOON document — DecodeTOONDocument should reject this.
	bogus := []byte("this is not toon at all\n")
	_, err := toon.DecodeTypedDocument(bogus, toon.Schema{})
	if err == nil {
		t.Fatal("DecodeTypedDocument(bogus)=nil err; want DecodeTOONDocument err propagation")
	}
	// Must NOT carry the typed-decode re-parse prefix — that's a different
	// arm. The propagation here is naked passthrough from DecodeTOONDocument.
	if strings.Contains(err.Error(), "typed decode re-parse:") {
		t.Errorf("err=%q; must not carry 'typed decode re-parse:' prefix (that's the post-schema arm)", err)
	}
}
