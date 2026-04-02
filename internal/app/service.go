package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"doppler-sniper/internal/detector"
	"doppler-sniper/internal/models"
)

const (
	MaxLaunchesToLoad  = 30
	BaseChainID        = 8453
	BaseSepoliaChainID = 84532
	StreamedAnalysisThreshold = 5000
	PartialAnalysisSwapCap    = 20000
	RetainedRecentSwaps       = 64
	RetainedWalletSwaps       = 24
)

type ExplorerRow struct {
	Launch   models.Launch
	Analysis models.LaunchAnalysis
}

type Repository interface {
	ListMigratedLaunches(ctx context.Context, filter models.LaunchFilter) ([]models.Launch, error)
	GetLaunchByAsset(ctx context.Context, assetAddress string, chainID int) (models.Launch, error)
	CountLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) (int, error)
	GetLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) ([]models.Swap, error)
	StreamLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64, pageSize int, consume func([]models.Swap) error) error
	Ping(ctx context.Context) error
	HasRequiredSchema(ctx context.Context) (bool, error)
	CountCheckpoints(ctx context.Context) (int, error)
	IsIndexerReady(ctx context.Context) (bool, error)
	GetLatestSwapTimestamp(ctx context.Context) (int64, error)
}

var ErrSchemaNotReady = errors.New("indexer schema not ready")
var ErrIndexerNotReady = errors.New("indexer has not started checkpointing")
var ErrAssetNotFound = errors.New("asset not found")
var ErrAssetNotAnalyzable = errors.New("asset is not sniper analyzable")

const trustedSwapBackedPoolType = "zora"

type Service struct {
	repo          Repository
	launchChainID int
	sourceLabel   string
	remote        bool
}

type ServiceConfig struct {
	LaunchChainID int
	SourceLabel   string
	Remote        bool
}

func NewService(repo Repository, cfg ServiceConfig) *Service {
	chainID := cfg.LaunchChainID
	if chainID == 0 {
		chainID = BaseChainID
	}
	return &Service{
		repo:          repo,
		launchChainID: chainID,
		sourceLabel:   cfg.SourceLabel,
		remote:        cfg.Remote,
	}
}

func (s *Service) LaunchChainID() int {
	return s.launchChainID
}

func (s *Service) SourceLabel() string {
	return s.sourceLabel
}

func (s *Service) IsRemote() bool {
	return s.remote
}

func (s *Service) LoadData(ctx context.Context) ([]ExplorerRow, []models.LeaderboardEntry, []models.CurveComparison, string, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, nil, nil, "", err
	}

	launches, err := s.repo.ListMigratedLaunches(ctx, models.LaunchFilter{
		ChainIDs:  []int{s.launchChainID},
		DateRange: "all",
		Limit:     MaxLaunchesToLoad,
	})
	if err != nil {
		return nil, nil, nil, "", err
	}

	rows := make([]ExplorerRow, 0, len(launches))
	grouped := map[string][]models.LaunchAnalysis{}
	skippedUnsupportedType := 0
	skippedZeroSwaps := 0
	leaders := map[string]*agg{}

	for _, launch := range launches {
		if !isSniperAnalyzableType(launch.Type) {
			skippedUnsupportedType++
			continue
		}

		swapCount, countErr := s.repo.CountLaunchSwaps(ctx, launch.Address, launch.ChainID, launch.MigratedAt)
		if countErr != nil {
			return nil, nil, nil, "", fmt.Errorf("count swaps for %s on chain %d: %w", launch.Address, launch.ChainID, countErr)
		}
		if swapCount == 0 {
			skippedZeroSwaps++
			continue
		}
		analysis, loadErr := s.loadAnalysis(ctx, launch, swapCount)
		if loadErr != nil {
			return nil, nil, nil, "", fmt.Errorf("load analysis for %s on chain %d: %w", launch.Address, launch.ChainID, loadErr)
		}
		rows = append(rows, ExplorerRow{Launch: launch, Analysis: analysis})
		curveType := NormalizeCurveType(launch.Type)
		grouped[curveType] = append(grouped[curveType], analysis)

		for _, wallet := range analysis.Wallets {
			if wallet.Classification != models.ClassificationSniper {
				continue
			}
			item := leaders[wallet.Wallet]
			if item == nil {
				item = &agg{chains: map[int]struct{}{}}
				leaders[wallet.Wallet] = item
			}
			item.profit += wallet.ProfitUSD
			item.count++
			item.entryPct += wallet.EntrySupplyPct
			item.chains[launch.ChainID] = struct{}{}
			if wallet.FirstBuyTS > item.recent {
				item.recent = wallet.FirstBuyTS
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Launch.MigratedAt > rows[j].Launch.MigratedAt
	})

	return rows, buildLeaderboard(leaders), buildComparisons(grouped), formatAnalyzableWarning(skippedUnsupportedType, skippedZeroSwaps), nil
}

func (s *Service) LoadAssetData(ctx context.Context, assetAddress string) ([]ExplorerRow, []models.LeaderboardEntry, []models.CurveComparison, string, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, nil, nil, "", err
	}

	assetAddress = strings.ToLower(strings.TrimSpace(assetAddress))
	if assetAddress == "" {
		return nil, nil, nil, "", fmt.Errorf("%w: empty address", ErrAssetNotFound)
	}

	launch, err := s.repo.GetLaunchByAsset(ctx, assetAddress, s.launchChainID)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if launch.Address == "" {
		return nil, nil, nil, "", fmt.Errorf("%w: %s on chain %d", ErrAssetNotFound, assetAddress, s.launchChainID)
	}
	if !isSniperAnalyzableType(launch.Type) {
		return nil, nil, nil, "", fmt.Errorf("%w: pool type %q is aggregate-only right now", ErrAssetNotAnalyzable, launch.Type)
	}

	swapCount, err := s.repo.CountLaunchSwaps(ctx, launch.Address, launch.ChainID, launch.MigratedAt)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("count swaps for %s on chain %d: %w", launch.Address, launch.ChainID, err)
	}
	if swapCount == 0 {
		return nil, nil, nil, "", fmt.Errorf("%w: no raw swaps were exposed for %s", ErrAssetNotAnalyzable, assetAddress)
	}

	analysis, err := s.loadAnalysis(ctx, launch, swapCount)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("load analysis for %s on chain %d: %w", launch.Address, launch.ChainID, err)
	}

	rows := []ExplorerRow{{Launch: launch, Analysis: analysis}}
	grouped := map[string][]models.LaunchAnalysis{
		NormalizeCurveType(launch.Type): {analysis},
	}

	leaders := map[string]*agg{}
	for _, wallet := range analysis.Wallets {
		if wallet.Classification != models.ClassificationSniper {
			continue
		}
		item := leaders[wallet.Wallet]
		if item == nil {
			item = &agg{chains: map[int]struct{}{}}
			leaders[wallet.Wallet] = item
		}
		item.profit += wallet.ProfitUSD
		item.count++
		item.entryPct += wallet.EntrySupplyPct
		item.chains[launch.ChainID] = struct{}{}
		if wallet.FirstBuyTS > item.recent {
			item.recent = wallet.FirstBuyTS
		}
	}

	warning := fmt.Sprintf("Loaded %s via pool %s with %d raw swaps", launch.Symbol, truncateAddress(launch.Address), swapCount)
	if swapCount > StreamedAnalysisThreshold {
		warning += fmt.Sprintf(" using streaming mode (showing last %d swaps)", RetainedRecentSwaps)
	}
	if swapCount > PartialAnalysisSwapCap {
		warning += fmt.Sprintf("; partial classification based on first %d chronological swaps", PartialAnalysisSwapCap)
	}
	return rows, buildLeaderboard(leaders), buildComparisons(grouped), warning, nil
}

type agg struct {
	profit   float64
	count    int
	entryPct float64
	recent   int64
	chains   map[int]struct{}
}

func buildLeaderboard(leaders map[string]*agg) []models.LeaderboardEntry {
	leaderboard := make([]models.LeaderboardEntry, 0, len(leaders))
	for wallet, agg := range leaders {
		chains := make([]int, 0, len(agg.chains))
		for chainID := range agg.chains {
			chains = append(chains, chainID)
		}
		sort.Ints(chains)
		avgEntry := 0.0
		if agg.count > 0 {
			avgEntry = agg.entryPct / float64(agg.count)
		}
		leaderboard = append(leaderboard, models.LeaderboardEntry{
			Wallet:            wallet,
			TotalProfitUSD:    agg.profit,
			LaunchesSniped:    agg.count,
			Chains:            chains,
			AvgEntrySupplyPct: avgEntry,
			MostRecentSnipeTS: agg.recent,
		})
	}
	sort.Slice(leaderboard, func(i, j int) bool {
		return leaderboard[i].TotalProfitUSD > leaderboard[j].TotalProfitUSD
	})
	return leaderboard
}

func (s *Service) ensureReady(ctx context.Context) error {
	schemaReady, err := s.repo.HasRequiredSchema(ctx)
	if err != nil {
		return err
	}
	if !schemaReady {
		return ErrSchemaNotReady
	}
	checkpoints, err := s.repo.CountCheckpoints(ctx)
	if err != nil {
		return err
	}
	indexerReady, err := s.repo.IsIndexerReady(ctx)
	if err != nil {
		return err
	}
	if checkpoints == 0 && !indexerReady {
		return ErrIndexerNotReady
	}
	return nil
}

func (s *Service) loadAnalysis(ctx context.Context, launch models.Launch, swapCount int) (models.LaunchAnalysis, error) {
	if swapCount == 0 {
		return models.LaunchAnalysis{Launch: launch}, nil
	}
	if swapCount > StreamedAnalysisThreshold {
		return s.loadAnalysisStreamed(ctx, launch, swapCount)
	}

	swaps, err := s.repo.GetLaunchSwaps(ctx, launch.Address, launch.ChainID, launch.MigratedAt)
	if err != nil {
		return models.LaunchAnalysis{}, err
	}
	if len(swaps) == 0 {
		return models.LaunchAnalysis{Launch: launch}, nil
	}
	return detector.AnalyzeLaunch(launch, swaps), nil
}

func (s *Service) loadAnalysisStreamed(ctx context.Context, launch models.Launch, swapCount int) (models.LaunchAnalysis, error) {
	maxToConsume := swapCount
	if maxToConsume > PartialAnalysisSwapCap {
		maxToConsume = PartialAnalysisSwapCap
	}
	var lastErr error
	for _, pageSize := range []int{1000, 500} {
		acc := detector.NewLaunchAccumulator(launch, detector.AccumulatorOptions{
			MaxRecentSwaps: RetainedRecentSwaps,
			MaxWalletSwaps: RetainedWalletSwaps,
		})
		consumed := 0
		err := s.repo.StreamLaunchSwaps(ctx, launch.Address, launch.ChainID, launch.MigratedAt, pageSize, func(swaps []models.Swap) error {
			if consumed >= maxToConsume {
				return errStopStreaming
			}
			remaining := maxToConsume - consumed
			if remaining < len(swaps) {
				swaps = swaps[:remaining]
			}
			acc.Consume(swaps)
			consumed += len(swaps)
			if consumed >= maxToConsume {
				return errStopStreaming
			}
			return nil
		})
		if errors.Is(err, errStopStreaming) {
			return acc.Finalize(), nil
		}
		if err == nil {
			return acc.Finalize(), nil
		}
		lastErr = err
		if !shouldRetryStream(err) {
			return models.LaunchAnalysis{}, err
		}
	}
	return models.LaunchAnalysis{}, lastErr
}

func truncateAddress(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:6] + "..." + value[len(value)-4:]
}

func isSniperAnalyzableType(poolType string) bool {
	return strings.EqualFold(strings.TrimSpace(poolType), trustedSwapBackedPoolType)
}

func formatAnalyzableWarning(skippedUnsupportedType, skippedZeroSwaps int) string {
	parts := make([]string, 0, 2)
	if skippedUnsupportedType > 0 {
		parts = append(parts, fmt.Sprintf("%d unsupported pool types", skippedUnsupportedType))
	}
	if skippedZeroSwaps > 0 {
		parts = append(parts, fmt.Sprintf("%d without raw swap history", skippedZeroSwaps))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Showing only sniper-analyzable markets: filtered " + strings.Join(parts, ", ")
}

func shouldRetryStream(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected error") || strings.Contains(msg, "context deadline exceeded")
}

var errStopStreaming = errors.New("stop streaming")
