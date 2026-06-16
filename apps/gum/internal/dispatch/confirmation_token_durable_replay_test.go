package dispatch

import (
	"path/filepath"
	"testing"
	"time"
)

func TestVerifyConfirmationTokenDurableReplaySurvivesMemoryReset(t *testing.T) {
	ResetReplayCacheForTest()
	t.Cleanup(ResetReplayCacheForTest)

	params := confirmationBindingParams(5 * time.Minute)
	params.ProfileName = "default"
	params.ReplayStoreDir = t.TempDir()
	params.RequireDurableReplay = true

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}
	if err := VerifyConfirmationToken(tok, params); err != nil {
		t.Fatalf("VerifyConfirmationToken first use: %v", err)
	}

	ResetReplayCacheForTest()
	assertTokenInvalid(t, VerifyConfirmationToken(tok, params), "replayed")

	replayDir := filepath.Join(params.ReplayStoreDir, "confirmation-replay")
	entries, err := filepath.Glob(filepath.Join(replayDir, "*"))
	if err != nil {
		t.Fatalf("glob replay dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("durable replay markers=%d, want 1", len(entries))
	}
}

func TestVerifyConfirmationTokenDurableReplayFailsClosedWithoutStore(t *testing.T) {
	params := confirmationBindingParams(5 * time.Minute)
	params.ProfileName = "default"
	params.RequireDurableReplay = true

	tok, err := IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}
	assertTokenInvalid(t, VerifyConfirmationToken(tok, params), "replay_store_unavailable")
}
