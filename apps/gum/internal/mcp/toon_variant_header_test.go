package mcp

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/toon"
)

// TestToonVariantHeader proves what a TOON result tells a client about the
// variant that produced it, and that the id it reports is an exact key into
// the catalog.
//
// Spec §13 line 2771 splits the answer in two: the `toon` string body carries
// the `count` and `fields` header values, and `op` and `variant` are
// duplicated as top-level keys on the ToonResult. The first two subtests cover
// the envelope and the id lookup; the third decodes the shaped body itself and
// reads the resolved variant back off its §9.0 header.
func TestToonVariantHeader(t *testing.T) {
	opID, variantID := firstCatalogVariant(t)

	t.Run("the ToonResult envelope carries the resolved op and variant", func(t *testing.T) {
		body := "id,n\n\"1\",2\n"
		vid := variantID
		shaped := &dispatch.ShapedResponse{
			Format: "toon",
			Body:   []byte(body),
			Expression: &dispatch.ExpressionMeta{
				Profile:     "gmail.messages.list.v1",
				OpID:        opID,
				VariantID:   &vid,
				ResultCount: 1,
			},
		}

		env, ok := tierAResult(shaped).(map[string]any)
		if !ok {
			t.Fatalf("tierAResult returned %T; want map[string]any", tierAResult(shaped))
		}
		if env["format"] != "toon" {
			t.Errorf("format=%v; want toon", env["format"])
		}
		if env["toon"] != body {
			t.Errorf("toon=%q; want the shaped body %q", env["toon"], body)
		}
		if env["op"] != opID {
			t.Errorf("op=%v; want %q", env["op"], opID)
		}
		if env["variant"] != variantID {
			t.Errorf("variant=%v; want the resolved variant %q", env["variant"], variantID)
		}

		// §13 closes ToonResult with additionalProperties:false and names the
		// body as the only home for count and fields. A tool that lifted
		// either to the top level would fail its own registered schema.
		for _, forbidden := range []string{"count", "fields", "data"} {
			if _, present := env[forbidden]; present {
				t.Errorf("ToonResult carries top-level %q; §13 forbids it", forbidden)
			}
		}
	})

	t.Run("the reported variant id is an exact catalog key", func(t *testing.T) {
		c := defaultCatalog()
		if c == nil {
			t.Fatal("defaultCatalog()=nil; the embedded catalog did not parse")
		}

		gotOp, gotVariant := findVariant(c, variantID)
		if gotVariant == nil {
			t.Fatalf("findVariant(%q) missed; the id the envelope reports must resolve", variantID)
		}
		if gotVariant.VariantID != variantID {
			t.Errorf("resolved variant_id=%q; want %q", gotVariant.VariantID, variantID)
		}
		if gotOp.OpID != opID {
			t.Errorf("resolved op_id=%q; want %q", gotOp.OpID, opID)
		}

		// Exact, not prefix or substring: the id owns one variant in the
		// whole snapshot, and a near-miss resolves to nothing.
		matches := 0
		for i := range c.Ops {
			for j := range c.Ops[i].Variants {
				if c.Ops[i].Variants[j].VariantID == variantID {
					matches++
				}
			}
		}
		if matches != 1 {
			t.Errorf("variant_id %q matches %d variants; want exactly 1", variantID, matches)
		}
		for _, near := range []string{variantID + ".x", strings.TrimSuffix(variantID, "t"), strings.ToUpper(variantID)} {
			if near == variantID {
				continue
			}
			if _, v := findVariant(c, near); v != nil {
				t.Errorf("findVariant(%q) resolved; lookup is not exact", near)
			}
		}
	})

	t.Run("the shaped TOON body is a 9.0 document naming the resolved variant", func(t *testing.T) {
		// §9.0 line 1953 measured against the encoder the response path
		// actually calls, not against the toon package directly.
		out, err := shapedTOONBody(t, opID, variantID)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := toon.DecodeTOONDocument([]byte(out))
		if err != nil {
			t.Fatalf("DecodeTOONDocument: %v;\nbody:\n%s", err, out)
		}
		if doc.Variant != variantID {
			t.Errorf("variant header = %q; want the resolved variant %q", doc.Variant, variantID)
		}
		if doc.Op != opID {
			t.Errorf("op header = %q; want %q", doc.Op, opID)
		}
		if doc.FormatVersion != 1 {
			t.Errorf("format_version = %d; want 1", doc.FormatVersion)
		}
		if doc.Count != 2 || len(doc.Rows) != 2 {
			t.Fatalf("count=%d rows=%d; want 2 and 2;\nbody:\n%s", doc.Count, len(doc.Rows), out)
		}
		if got := strings.Join(doc.Fields, ","); got != "from,id,subject" {
			t.Errorf("fields = %q; want \"from,id,subject\"", got)
		}

		// No CSV header row: the first body line is data, and §9.0 gives the
		// column order in the fields header instead.
		_, body, found := strings.Cut(out, "\n\n")
		if !found {
			t.Fatalf("body has no blank-line separator;\nbody:\n%s", out)
		}
		if first, _, _ := strings.Cut(body, "\n"); first == "from,id,subject" {
			t.Errorf("body repeats the fields as a CSV header row; §9.0 forbids it;\nbody:\n%s", out)
		}
	})
}

// firstCatalogVariant returns the op_id and default variant_id of the first
// embedded-catalog op that declares one. Deriving both from the snapshot
// keeps the envelope assertion and the lookup assertion on the same id.
func firstCatalogVariant(t *testing.T) (string, string) {
	t.Helper()
	c := defaultCatalog()
	if c == nil {
		t.Fatal("defaultCatalog()=nil; the embedded catalog did not parse")
	}
	for i := range c.Ops {
		op := &c.Ops[i]
		if op.DefaultVariantID == "" {
			continue
		}
		for j := range op.Variants {
			if op.Variants[j].VariantID == op.DefaultVariantID {
				return op.OpID, op.DefaultVariantID
			}
		}
	}
	t.Fatal("no embedded op declares a resolvable default variant")
	return "", ""
}

// shapedTOONBody runs the real stage-8 encode for format "toon" and returns
// the bytes a caller receives. It goes through profile.Apply rather than the
// toon package so the probe cannot drift from the response path.
func shapedTOONBody(t *testing.T, opID, variantID string) (string, error) {
	t.Helper()
	body := []byte(`{"messages":[` +
		`{"id":"18a3f2b","from":"alice@example.com","subject":"Re: meeting"},` +
		`{"id":"19c4d3e","from":"bob@example.com","subject":"Budget"}` +
		`]}`)
	out, err := profile.Apply(
		&profile.Profile{Name: "toon_variant_header_probe", DefaultFormat: "toon", Flatten: true},
		profile.ApplyInput{Body: body, Op: opID, Variant: variantID},
	)
	if err != nil {
		return "", err
	}
	if out.Format != "toon" {
		t.Fatalf("stage 8 reported format=%q; want toon", out.Format)
	}
	if out.ResultCount != 2 {
		t.Fatalf("result_count=%d; want 2, the probe did not reach the record path", out.ResultCount)
	}
	return string(out.Body), nil
}
