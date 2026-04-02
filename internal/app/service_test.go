package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"doppler-sniper/internal/models"
)

type fakeRepo struct {
	launches      []models.Launch
	swapCounts    map[string]int
	swaps         map[string][]models.Swap
	schemaReady   bool
	listErr       error
	countErr      error
	getSwapsErr   error
	pingErr       error
	latestSwapTS  int64
	latestSwapErr error
	checkpoints   int
	indexerReady  bool
}

func (f *fakeRepo) ListMigratedLaunches(ctx context.Context, filter models.LaunchFilter) ([]models.Launch, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.launches, nil
}

func (f *fakeRepo) CountLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) (int, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.swapCounts[repoKey(poolAddress, chainID)], nil
}

func (f *fakeRepo) GetLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) ([]models.Swap, error) {
	if f.getSwapsErr != nil {
		return nil, f.getSwapsErr
	}
	return f.swaps[repoKey(poolAddress, chainID)], nil
}

func (f *fakeRepo) Ping(ctx context.Context) error {
	return f.pingErr
}

func (f *fakeRepo) HasRequiredSchema(ctx context.Context) (bool, error) {
	return f.schemaReady, nil
}

func (f *fakeRepo) CountCheckpoints(ctx context.Context) (int, error) {
	return f.checkpoints, nil
}

func (f *fakeRepo) IsIndexerReady(ctx context.Context) (bool, error) {
	return f.indexerReady, nil
}

func (f *fakeRepo) GetLatestSwapTimestamp(ctx context.Context) (int64, error) {
	if f.latestSwapErr != nil {
		return 0, f.latestSwapErr
	}
	return f.latestSwapTS, nil
}

func TestServiceLoadDataBuildsSummaries(t *testing.T) {
	repo := &fakeRepo{
		launches: []models.Launch{
			{
				Address:         "0xpool1",
				ChainID:         8453,
				Type:            "multicurve",
				MigratedAt:      200,
				TotalTokensSold: 1000,
				Symbol:          "AAA",
			},
			{
				Address:         "0xpool2",
				ChainID:         1,
				Type:            "v3",
				MigratedAt:      300,
				TotalTokensSold: 1000,
				Symbol:          "BBB",
			},
		},
		swapCounts: map[string]int{
			repoKey("0xpool1", 8453): 3,
			repoKey("0xpool2", 1):    2,
		},
		swaps: map[string][]models.Swap{
			repoKey("0xpool1", 8453): {
				{TxHash: "0x1", Pool: "0xpool1", ChainID: 8453, User: "0xsniper", Type: "buy", AmountOut: 200, SwapValueUSD: 1_000_000_000_000_000_000, Timestamp: 100},
				{TxHash: "0x2", Pool: "0xpool1", ChainID: 8453, User: "0xsniper", Type: "sell", AmountIn: 100, SwapValueUSD: 2_000_000_000_000_000_000, Timestamp: 150},
				{TxHash: "0x3", Pool: "0xpool1", ChainID: 8453, User: "0xholder", Type: "buy", AmountOut: 100, SwapValueUSD: 500_000_000_000_000_000, Timestamp: 160},
			},
			repoKey("0xpool2", 1): {
				{TxHash: "0x4", Pool: "0xpool2", ChainID: 1, User: "0xsniper", Type: "buy", AmountOut: 300, SwapValueUSD: 800_000_000_000_000_000, Timestamp: 210},
				{TxHash: "0x5", Pool: "0xpool2", ChainID: 1, User: "0xsniper", Type: "sell", AmountIn: 200, SwapValueUSD: 1_200_000_000_000_000_000, Timestamp: 260},
			},
		},
		schemaReady: true,
		checkpoints: 1,
	}

	service := NewService(repo, ServiceConfig{})
	rows, leaderboard, comparisons, err := service.LoadData(context.Background())
	if err != nil {
		t.Fatalf("LoadData returned error: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Launch.Address != "0xpool2" {
		t.Fatalf("expected most recent launch first, got %s", rows[0].Launch.Address)
	}
	if len(leaderboard) != 1 {
		t.Fatalf("expected 1 leaderboard entry, got %d", len(leaderboard))
	}
	if leaderboard[0].Wallet != "0xsniper" {
		t.Fatalf("expected sniper wallet in leaderboard, got %s", leaderboard[0].Wallet)
	}
	if leaderboard[0].LaunchesSniped != 2 {
		t.Fatalf("expected 2 snipes, got %d", leaderboard[0].LaunchesSniped)
	}
	if len(leaderboard[0].Chains) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(leaderboard[0].Chains))
	}
	if len(comparisons) != 3 {
		t.Fatalf("expected 3 comparisons, got %d", len(comparisons))
	}
	if comparisons[0].CurveType != "multicurve" || comparisons[0].LaunchCount != 1 {
		t.Fatalf("expected multicurve comparison with 1 launch, got %+v", comparisons[0])
	}
	if comparisons[2].CurveType != "v3" || comparisons[2].LaunchCount != 1 {
		t.Fatalf("expected v3 comparison with 1 launch, got %+v", comparisons[2])
	}
}

func TestServiceLoadDataFailsWhenAnalysisFails(t *testing.T) {
	repo := &fakeRepo{
		launches: []models.Launch{
			{Address: "0xpool1", ChainID: 8453, MigratedAt: 100},
		},
		schemaReady: true,
		checkpoints: 1,
		countErr:    errors.New("db unavailable"),
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, err := service.LoadData(context.Background())
	if err == nil {
		t.Fatal("expected LoadData to return error")
	}
}

func TestServiceLoadDataRejectsOversizedLaunch(t *testing.T) {
	repo := &fakeRepo{
		launches: []models.Launch{
			{Address: "0xpool1", ChainID: 8453, MigratedAt: 100},
		},
		schemaReady: true,
		checkpoints: 1,
		swapCounts: map[string]int{
			repoKey("0xpool1", 8453): MaxSwapsPerLaunch + 1,
		},
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, err := service.LoadData(context.Background())
	if err == nil {
		t.Fatal("expected oversized launch to return error")
	}
}

func TestServiceLoadDataWaitsForCheckpointing(t *testing.T) {
	repo := &fakeRepo{
		schemaReady: true,
		checkpoints: 0,
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, err := service.LoadData(context.Background())
	if !errors.Is(err, ErrIndexerNotReady) {
		t.Fatalf("expected ErrIndexerNotReady, got %v", err)
	}
}

func repoKey(address string, chainID int) string {
	return fmt.Sprintf("%s|%d", address, chainID)
}
