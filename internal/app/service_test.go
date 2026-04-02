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
	assetLaunches map[string]models.Launch
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

func (f *fakeRepo) GetLaunchByAsset(ctx context.Context, assetAddress string, chainID int) (models.Launch, error) {
	if f.listErr != nil {
		return models.Launch{}, f.listErr
	}
	return f.assetLaunches[repoKey(assetAddress, chainID)], nil
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

func (f *fakeRepo) StreamLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64, pageSize int, consume func([]models.Swap) error) error {
	if f.getSwapsErr != nil {
		return f.getSwapsErr
	}
	source := f.swaps[repoKey(poolAddress, chainID)]
	if pageSize <= 0 {
		pageSize = len(source)
	}
	for start := 0; start < len(source); start += pageSize {
		end := start + pageSize
		if end > len(source) {
			end = len(source)
		}
		if err := consume(source[start:end]); err != nil {
			return err
		}
	}
	return nil
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
				Type:            "zora",
				MigratedAt:      200,
				TotalTokensSold: 1000,
				Symbol:          "AAA",
			},
			{
				Address:         "0xpool2",
				ChainID:         1,
				Type:            "zora",
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
	rows, leaderboard, comparisons, warning, err := service.LoadData(context.Background())
	if err != nil {
		t.Fatalf("LoadData returned error: %v", err)
	}
	if warning != "" {
		t.Fatalf("expected no warning, got %q", warning)
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
	if comparisons[1].CurveType != "standard-v4" || comparisons[1].LaunchCount != 2 {
		t.Fatalf("expected standard-v4 comparison with 2 launches, got %+v", comparisons[1])
	}
}

func TestServiceLoadDataFailsWhenAnalysisFails(t *testing.T) {
	repo := &fakeRepo{
		launches: []models.Launch{
			{Address: "0xpool1", ChainID: 8453, Type: "zora", MigratedAt: 100, TotalTokensSold: 1000},
		},
		schemaReady: true,
		checkpoints: 1,
		swapCounts: map[string]int{
			repoKey("0xpool1", 8453): 1,
		},
		countErr: errors.New("db unavailable"),
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, _, err := service.LoadData(context.Background())
	if err == nil {
		t.Fatal("expected LoadData to return error")
	}
}

func TestServiceLoadDataSupportsLargeLaunches(t *testing.T) {
	repo := &fakeRepo{
		launches: []models.Launch{
			{Address: "0xpool1", ChainID: 8453, Type: "zora", MigratedAt: 100, TotalTokensSold: 1000, Symbol: "AAA"},
		},
		schemaReady: true,
		checkpoints: 1,
		swapCounts: map[string]int{
			repoKey("0xpool1", 8453): 6001,
		},
		swaps: map[string][]models.Swap{
			repoKey("0xpool1", 8453): {
				{TxHash: "0x1", Pool: "0xpool1", ChainID: 8453, User: "0xsniper", Type: "buy", AmountOut: 700, SwapValueUSD: 1_000_000_000_000_000_000, Timestamp: 90},
				{TxHash: "0x2", Pool: "0xpool1", ChainID: 8453, User: "0xsniper", Type: "sell", AmountIn: 400, SwapValueUSD: 1_500_000_000_000_000_000, Timestamp: 95},
			},
		},
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, warning, err := service.LoadData(context.Background())
	if err != nil {
		t.Fatalf("expected large launch to load, got error %v", err)
	}
	if warning != "" {
		t.Fatalf("expected no warning, got %q", warning)
	}
}

func TestServiceLoadDataFiltersNonAnalyzableLaunches(t *testing.T) {
	repo := &fakeRepo{
		launches: []models.Launch{
			{Address: "0xunsupported", ChainID: 8453, Type: "rehype", MigratedAt: 0, TotalTokensSold: 0, Symbol: "MISS"},
			{Address: "0xzero-swaps", ChainID: 8453, Type: "zora", MigratedAt: 300, TotalTokensSold: 0, Symbol: "NOSWAP"},
			{Address: "0xgood", ChainID: 8453, Type: "zora", MigratedAt: 0, TotalTokensSold: 0, Symbol: "GOOD"},
		},
		schemaReady: true,
		checkpoints: 1,
		swapCounts: map[string]int{
			repoKey("0xzero-swaps", 8453): 0,
			repoKey("0xgood", 8453):       2,
		},
		swaps: map[string][]models.Swap{
			repoKey("0xgood", 8453): {
				{TxHash: "0x1", Pool: "0xgood", ChainID: 8453, User: "0xsniper", Type: "buy", AmountOut: 700, SwapValueUSD: 1_000_000_000_000_000_000, Timestamp: 350},
				{TxHash: "0x2", Pool: "0xgood", ChainID: 8453, User: "0xsniper", Type: "sell", AmountIn: 400, SwapValueUSD: 1_500_000_000_000_000_000, Timestamp: 360},
			},
		},
	}

	service := NewService(repo, ServiceConfig{})
	rows, _, _, warning, err := service.LoadData(context.Background())
	if err != nil {
		t.Fatalf("expected analyzable launches to load, got error %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 analyzable row, got %d", len(rows))
	}
	if rows[0].Launch.Address != "0xgood" {
		t.Fatalf("expected analyzable launch to remain, got %s", rows[0].Launch.Address)
	}
	expected := "Showing only sniper-analyzable markets: filtered 1 unsupported pool types, 1 without raw swap history"
	if warning != expected {
		t.Fatalf("expected warning %q, got %q", expected, warning)
	}
}

func TestServiceLoadDataWaitsForCheckpointing(t *testing.T) {
	repo := &fakeRepo{
		schemaReady: true,
		checkpoints: 0,
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, _, err := service.LoadData(context.Background())
	if !errors.Is(err, ErrIndexerNotReady) {
		t.Fatalf("expected ErrIndexerNotReady, got %v", err)
	}
}

func TestServiceLoadAssetDataLoadsSingleAnalyzableAsset(t *testing.T) {
	repo := &fakeRepo{
		assetLaunches: map[string]models.Launch{
			repoKey("0xasset", 8453): {Address: "0xpool1", Asset: "0xasset", ChainID: 8453, Type: "zora", Symbol: "AAA", Name: "Asset AAA"},
		},
		schemaReady: true,
		checkpoints: 1,
		swapCounts: map[string]int{
			repoKey("0xpool1", 8453): 2,
		},
		swaps: map[string][]models.Swap{
			repoKey("0xpool1", 8453): {
				{TxHash: "0x1", Pool: "0xpool1", ChainID: 8453, User: "0xsniper", Type: "buy", AmountOut: 700, SwapValueUSD: 1_000_000_000_000_000_000, Timestamp: 350},
				{TxHash: "0x2", Pool: "0xpool1", ChainID: 8453, User: "0xsniper", Type: "sell", AmountIn: 400, SwapValueUSD: 1_500_000_000_000_000_000, Timestamp: 360},
			},
		},
	}

	service := NewService(repo, ServiceConfig{})
	rows, leaderboard, comparisons, warning, err := service.LoadAssetData(context.Background(), "0xasset")
	if err != nil {
		t.Fatalf("expected asset load to succeed, got %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Launch.Address != "0xpool1" {
		t.Fatalf("expected canonical pool, got %s", rows[0].Launch.Address)
	}
	if len(leaderboard) != 1 {
		t.Fatalf("expected 1 leaderboard entry, got %d", len(leaderboard))
	}
	if len(comparisons) != 3 {
		t.Fatalf("expected 3 comparisons, got %d", len(comparisons))
	}
	if warning == "" {
		t.Fatal("expected asset load warning/summary to be populated")
	}
}

func TestServiceLoadAssetDataRejectsUnsupportedPoolType(t *testing.T) {
	repo := &fakeRepo{
		assetLaunches: map[string]models.Launch{
			repoKey("0xasset", 8453): {Address: "0xpool1", Asset: "0xasset", ChainID: 8453, Type: "rehype", Symbol: "AAA"},
		},
		schemaReady: true,
		checkpoints: 1,
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, _, err := service.LoadAssetData(context.Background(), "0xasset")
	if !errors.Is(err, ErrAssetNotAnalyzable) {
		t.Fatalf("expected ErrAssetNotAnalyzable, got %v", err)
	}
}

func TestServiceLoadAssetDataRejectsAssetsWithoutRawSwaps(t *testing.T) {
	repo := &fakeRepo{
		assetLaunches: map[string]models.Launch{
			repoKey("0xasset", 8453): {Address: "0xpool1", Asset: "0xasset", ChainID: 8453, Type: "zora", Symbol: "AAA"},
		},
		schemaReady: true,
		checkpoints: 1,
		swapCounts: map[string]int{
			repoKey("0xpool1", 8453): 0,
		},
	}

	service := NewService(repo, ServiceConfig{})
	_, _, _, _, err := service.LoadAssetData(context.Background(), "0xasset")
	if !errors.Is(err, ErrAssetNotAnalyzable) {
		t.Fatalf("expected ErrAssetNotAnalyzable, got %v", err)
	}
}

func repoKey(address string, chainID int) string {
	return fmt.Sprintf("%s|%d", address, chainID)
}
