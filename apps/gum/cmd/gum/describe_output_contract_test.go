package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestDescribeEmitsCatalogEntryWhole pins the §12 automation-safe output row
// for `gum describe`: the command prints the op's catalog entry plus a
// synthesised arg map on every run, with no output flag to select.
//
// The variant count is the load-bearing assertion. `gum.describe_op` collapses
// variants[] at max_variants; the CLI does not, and a caller picks a variant by
// reading the printed list. A future collapse here would silently hide
// variants from every script that reads this output.
func TestDescribeEmitsCatalogEntryWhole(t *testing.T) {
	c := loadCatalog()
	if c == nil || len(c.Ops) == 0 {
		t.Fatal("embedded catalog is empty")
	}

	widest := &c.Ops[0]
	for i := range c.Ops {
		if len(c.Ops[i].Variants) > len(widest.Variants) {
			widest = &c.Ops[i]
		}
	}

	out, err := runCLI(t, "describe", widest.OpID)
	if err != nil {
		t.Fatalf("gum describe %s: %v", widest.OpID, err)
	}

	var got struct {
		Op struct {
			OpID     string            `json:"op_id"`
			Variants []json.RawMessage `json:"variants"`
		} `json:"op"`
		ExampleArgs map[string]any `json:"example_args"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode describe output: %v (out=%q)", err, out)
	}

	if got.Op.OpID != widest.OpID {
		t.Errorf("op.op_id = %q; want %q", got.Op.OpID, widest.OpID)
	}

	if len(got.Op.Variants) != len(widest.Variants) {
		t.Errorf("op.variants has %d entries; want the catalog's %d, uncollapsed",
			len(got.Op.Variants), len(widest.Variants))
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatalf("decode describe root: %v", err)
	}

	root := make([]string, 0, len(keys))
	for k := range keys {
		root = append(root, k)
	}
	sort.Strings(root)

	if strings.Join(root, ",") != "example_args,op" {
		t.Errorf("describe root keys = %v; want [example_args op]", root)
	}
}

// TestDescribeTakesNoOutputOrVariantFlag pins the other half of the same row:
// §12 lists `gum describe` among the commands that print their JSON form
// unconditionally, and §12.0 records that the CLI has no `--variant-id`. Both
// are claims a reader will try, so both fail here if a flag appears.
func TestDescribeTakesNoOutputOrVariantFlag(t *testing.T) {
	c := loadCatalog()
	if c == nil || len(c.Ops) == 0 {
		t.Fatal("embedded catalog is empty")
	}
	opID := c.Ops[0].OpID

	for _, flag := range []string{"--format=json", "--variant-id=" + opID} {
		t.Run(flag, func(t *testing.T) {
			if _, err := runCLI(t, "describe", opID, flag); err == nil {
				t.Errorf("gum describe %s %s succeeded; want an unknown-flag error", opID, flag)
			}
		})
	}
}
