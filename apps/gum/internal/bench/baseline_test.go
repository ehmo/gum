package bench_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// wantLiveToolEntryCount is the expected number of entries in
// tier-a-token-baseline.json: 27 Tier A tools plus 2 skills helper tools.
const wantLiveToolEntryCount = 29

// TestTierABaselineJSONLoads asserts that testdata/tier-a-token-baseline.json
// (spec §2 line 129, bead gum-coo) exists, parses as valid JSON, and has
// exactly 29 entries (27 Tier A tools plus 2 skills helper tools).
func TestTierABaselineJSONLoads(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	baselinePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "tier-a-token-baseline.json")

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read tier-a-token-baseline.json: %v", err)
	}

	var baseline map[string]int
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("unmarshal tier-a-token-baseline.json: %v", err)
	}

	if len(baseline) != wantLiveToolEntryCount {
		t.Errorf("tier-a-token-baseline.json has %d entries, want %d (27 Tier A + 2 skills helpers)",
			len(baseline), wantLiveToolEntryCount)
	}
}
