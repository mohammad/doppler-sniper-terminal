package indexerapi

import (
	"context"
	"testing"
)

func TestProblematicPoolSwapFetch(t *testing.T) {
	t.Parallel()

	repo := New("")
	ctx := context.Background()

	const (
		poolAddress = "0x10ad36f5c84ed3f59c28e1eee589bbee739a38e0"
		chainID     = 84532
		migratedAt  = int64(1774457352)
	)

	count, err := repo.CountLaunchSwaps(ctx, poolAddress, chainID, migratedAt)
	if err != nil {
		t.Fatalf("CountLaunchSwaps returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected zero pre-migration swaps, got %d", count)
	}

	swaps, err := repo.GetLaunchSwaps(ctx, poolAddress, chainID, migratedAt)
	if err != nil {
		t.Fatalf("GetLaunchSwaps returned error: %v", err)
	}
	if len(swaps) != 0 {
		t.Fatalf("expected zero pre-migration swaps, got %d", len(swaps))
	}
}
