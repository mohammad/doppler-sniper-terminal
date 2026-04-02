package detector

import (
	"sort"
	"time"

	"doppler-sniper/internal/metrics"
	"doppler-sniper/internal/models"
)

type AccumulatorOptions struct {
	MaxRecentSwaps int
	MaxWalletSwaps int
}

type walletAggregate struct {
	all            []models.Swap
	buys           []models.Swap
	sells          []models.Swap
	firstBuyTS     int64
	lastSellTS     int64
	entrySupplyPct float64
	usdSpent       float64
	usdReceived    float64
	tokensBought   int64
	tokensSold     int64
}

type LaunchAccumulator struct {
	launch               models.Launch
	opts                 AccumulatorOptions
	thresholdTokens      int64
	earlyThresholdTS     int64
	thresholdReached     bool
	lastSeenTimestamp    int64
	cumulativeSold       int64
	totalPreMigrationUSD float64
	walletAgg            map[string]*walletAggregate
	preMigrationSwaps    []models.Swap
}

func AnalyzeLaunch(launch models.Launch, swaps []models.Swap) models.LaunchAnalysis {
	sort.Slice(swaps, func(i, j int) bool {
		if swaps[i].Timestamp == swaps[j].Timestamp {
			return swaps[i].TxHash < swaps[j].TxHash
		}
		return swaps[i].Timestamp < swaps[j].Timestamp
	})

	acc := NewLaunchAccumulator(launch, AccumulatorOptions{
		MaxRecentSwaps: len(swaps),
		MaxWalletSwaps: len(swaps),
	})
	acc.Consume(swaps)
	return acc.Finalize()
}

func NewLaunchAccumulator(launch models.Launch, opts AccumulatorOptions) *LaunchAccumulator {
	if opts.MaxRecentSwaps < 0 {
		opts.MaxRecentSwaps = 0
	}
	if opts.MaxWalletSwaps < 0 {
		opts.MaxWalletSwaps = 0
	}
	return &LaunchAccumulator{
		launch:           launch,
		opts:             opts,
		thresholdTokens:  int64(float64(launch.TotalTokensSold) * 0.20),
		earlyThresholdTS: launch.MigratedAt,
		walletAgg:        map[string]*walletAggregate{},
		preMigrationSwaps: make([]models.Swap, 0, max(0, opts.MaxRecentSwaps)),
	}
}

func (a *LaunchAccumulator) Consume(swaps []models.Swap) {
	for _, swap := range swaps {
		a.consumeSwap(swap)
	}
}

func (a *LaunchAccumulator) Finalize() models.LaunchAnalysis {
	earlyThresholdTS := a.earlyThresholdTS
	if earlyThresholdTS == 0 && a.lastSeenTimestamp > 0 {
		earlyThresholdTS = a.lastSeenTimestamp
	}

	wallets := make([]models.WalletLaunchStats, 0, len(a.walletAgg))
	walletsByAddress := make(map[string]models.WalletLaunchStats, len(a.walletAgg))
	var sniperBuyUSD float64

	for wallet, agg := range a.walletAgg {
		if len(agg.buys) == 0 && agg.tokensBought == 0 {
			continue
		}

		sellRatio := 0.0
		if agg.tokensBought > 0 {
			sellRatio = float64(agg.tokensSold) / float64(agg.tokensBought)
		}

		classification := models.ClassificationLate
		isEarly := agg.firstBuyTS > 0 && agg.firstBuyTS <= earlyThresholdTS
		if isEarly && sellRatio >= 0.50 {
			classification = models.ClassificationSniper
		} else if isEarly && sellRatio < 0.20 {
			classification = models.ClassificationBeliever
		} else if isEarly {
			classification = models.ClassificationMixed
		}

		if classification == models.ClassificationSniper {
			sniperBuyUSD += agg.usdSpent
		}

		duration := time.Duration(0)
		if agg.firstBuyTS > 0 && agg.lastSellTS > agg.firstBuyTS {
			duration = time.Duration(agg.lastSellTS-agg.firstBuyTS) * time.Second
		}

		stat := models.WalletLaunchStats{
			Wallet:            wallet,
			Classification:    classification,
			FirstBuyTS:        agg.firstBuyTS,
			LastSellTS:        agg.lastSellTS,
			EntrySupplyPct:    agg.entrySupplyPct,
			USDSpent:          agg.usdSpent,
			USDReceived:       agg.usdReceived,
			ProfitUSD:         agg.usdReceived - agg.usdSpent,
			TokensBought:      agg.tokensBought,
			TokensSold:        agg.tokensSold,
			SellRatio:         sellRatio,
			Duration:          duration,
			Swaps:             append([]models.Swap(nil), agg.all...),
			PreMigrationBuys:  append([]models.Swap(nil), agg.buys...),
			PreMigrationSells: append([]models.Swap(nil), agg.sells...),
		}

		wallets = append(wallets, stat)
		walletsByAddress[wallet] = stat
	}

	sort.Slice(wallets, func(i, j int) bool {
		if wallets[i].Classification == wallets[j].Classification {
			return wallets[i].ProfitUSD > wallets[j].ProfitUSD
		}
		return wallets[i].EntrySupplyPct < wallets[j].EntrySupplyPct
	})

	sniperCount := 0
	believerCount := 0
	for _, wallet := range wallets {
		if wallet.Classification == models.ClassificationSniper {
			sniperCount++
		}
		if wallet.Classification == models.ClassificationBeliever {
			believerCount++
		}
	}

	return models.LaunchAnalysis{
		Launch:               a.launch,
		EarlyThresholdTS:     earlyThresholdTS,
		Wallets:              wallets,
		WalletsByAddress:     walletsByAddress,
		PreMigrationSwaps:    append([]models.Swap(nil), a.preMigrationSwaps...),
		SniperPressureScore:  metrics.PressureScore(sniperBuyUSD, a.totalPreMigrationUSD),
		TotalSnipedUSD:       totalSnipedUSD(wallets),
		TotalPreMigrationUSD: a.totalPreMigrationUSD,
		SniperCount:          sniperCount,
		BelieverCount:        believerCount,
		BuyerCount:           len(wallets),
		BelieverLossUSD:      metrics.BelieverLossUSD(wallets),
	}
}

func (a *LaunchAccumulator) consumeSwap(swap models.Swap) {
	if a.launch.MigratedAt != 0 && swap.Timestamp > a.launch.MigratedAt {
		return
	}
	a.lastSeenTimestamp = swap.Timestamp
	appendTailSwap(&a.preMigrationSwaps, swap, a.opts.MaxRecentSwaps)

	agg := a.walletAgg[swap.User]
	if agg == nil {
		agg = &walletAggregate{}
		a.walletAgg[swap.User] = agg
	}
	appendTailSwap(&agg.all, swap, a.opts.MaxWalletSwaps)

	valueUSD := metrics.USDFromWAD(swap.SwapValueUSD)
	a.totalPreMigrationUSD += valueUSD

	if swap.Type == "buy" {
		before := a.cumulativeSold
		a.cumulativeSold += swap.AmountOut
		appendTailSwap(&agg.buys, swap, a.opts.MaxWalletSwaps)
		agg.usdSpent += valueUSD
		agg.tokensBought += swap.AmountOut
		if agg.firstBuyTS == 0 || swap.Timestamp < agg.firstBuyTS {
			agg.firstBuyTS = swap.Timestamp
			if a.launch.TotalTokensSold > 0 {
				agg.entrySupplyPct = (float64(before) / float64(a.launch.TotalTokensSold)) * 100
			}
		}
		if !a.thresholdReached && a.thresholdTokens > 0 && a.cumulativeSold >= a.thresholdTokens {
			a.thresholdReached = true
			a.earlyThresholdTS = swap.Timestamp
		}
	}

	if swap.Type == "sell" {
		appendTailSwap(&agg.sells, swap, a.opts.MaxWalletSwaps)
		agg.usdReceived += valueUSD
		agg.tokensSold += swap.AmountIn
		if swap.Timestamp > agg.lastSellTS {
			agg.lastSellTS = swap.Timestamp
		}
	}
}

func totalSnipedUSD(wallets []models.WalletLaunchStats) float64 {
	var total float64
	for _, wallet := range wallets {
		if wallet.Classification == models.ClassificationSniper && wallet.ProfitUSD > 0 {
			total += wallet.ProfitUSD
		}
	}
	return total
}

func appendTailSwap(dst *[]models.Swap, swap models.Swap, limit int) {
	if limit == 0 {
		return
	}
	*dst = append(*dst, swap)
	if limit > 0 && len(*dst) > limit {
		copy((*dst)[0:], (*dst)[1:])
		*dst = (*dst)[:limit]
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
