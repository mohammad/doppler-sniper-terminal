package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"doppler-sniper/internal/detector"
	"doppler-sniper/internal/models"
)

const (
	MaxLaunchesToLoad  = 100
	MaxSwapsPerLaunch  = 5000
	BaseChainID        = 8453
	BaseSepoliaChainID = 84532
)

type ExplorerRow struct {
	Launch   models.Launch
	Analysis models.LaunchAnalysis
}

type Repository interface {
	ListMigratedLaunches(ctx context.Context, filter models.LaunchFilter) ([]models.Launch, error)
	CountLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) (int, error)
	GetLaunchSwaps(ctx context.Context, poolAddress string, chainID int, migratedAt int64) ([]models.Swap, error)
	Ping(ctx context.Context) error
	HasRequiredSchema(ctx context.Context) (bool, error)
	CountCheckpoints(ctx context.Context) (int, error)
	IsIndexerReady(ctx context.Context) (bool, error)
	GetLatestSwapTimestamp(ctx context.Context) (int64, error)
}

var ErrSchemaNotReady = errors.New("indexer schema not ready")
var ErrIndexerNotReady = errors.New("indexer has not started checkpointing")

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

func (s *Service) LoadData(ctx context.Context) ([]ExplorerRow, []models.LeaderboardEntry, []models.CurveComparison, error) {
	schemaReady, err := s.repo.HasRequiredSchema(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if !schemaReady {
		return nil, nil, nil, ErrSchemaNotReady
	}
	checkpoints, err := s.repo.CountCheckpoints(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	indexerReady, err := s.repo.IsIndexerReady(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if checkpoints == 0 && !indexerReady {
		return nil, nil, nil, ErrIndexerNotReady
	}

	launches, err := s.repo.ListMigratedLaunches(ctx, models.LaunchFilter{
		ChainIDs:  []int{s.launchChainID},
		DateRange: "all",
		Limit:     MaxLaunchesToLoad,
	})
	if err != nil {
		return nil, nil, nil, err
	}

	rows := make([]ExplorerRow, 0, len(launches))
	grouped := map[string][]models.LaunchAnalysis{}
	type agg struct {
		profit   float64
		count    int
		entryPct float64
		recent   int64
		chains   map[int]struct{}
	}
	leaders := map[string]*agg{}

	for _, launch := range launches {
		analysis, loadErr := s.loadAnalysis(ctx, launch)
		if loadErr != nil {
			return nil, nil, nil, fmt.Errorf("load analysis for %s on chain %d: %w", launch.Address, launch.ChainID, loadErr)
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

	return rows, leaderboard, buildComparisons(grouped), nil
}

func (s *Service) loadAnalysis(ctx context.Context, launch models.Launch) (models.LaunchAnalysis, error) {
	swapCount, err := s.repo.CountLaunchSwaps(ctx, launch.Address, launch.ChainID, launch.MigratedAt)
	if err != nil {
		return models.LaunchAnalysis{}, err
	}
	if swapCount > MaxSwapsPerLaunch {
		return models.LaunchAnalysis{}, fmt.Errorf("launch has %d pre-migration swaps, exceeds safety cap of %d", swapCount, MaxSwapsPerLaunch)
	}
	if swapCount == 0 {
		return models.LaunchAnalysis{Launch: launch}, nil
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
