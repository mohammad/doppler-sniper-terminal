package detector

import (
	"sort"
	"time"

	"doppler-sniper/internal/metrics"
	"doppler-sniper/internal/models"
)

func AnalyzeLaunch(launch models.Launch, swaps []models.Swap) models.LaunchAnalysis {
	sort.Slice(swaps, func(i, j int) bool {
		if swaps[i].Timestamp == swaps[j].Timestamp {
			return swaps[i].TxHash < swaps[j].TxHash
		}
		return swaps[i].Timestamp < swaps[j].Timestamp
	})

	preMigrationSwaps := make([]models.Swap, 0, len(swaps))
	thresholdTokens := int64(float64(launch.TotalTokensSold) * 0.20)
	var cumulativeSold int64
	earlyThresholdTS := launch.MigratedAt
	if earlyThresholdTS == 0 && len(swaps) > 0 {
		earlyThresholdTS = swaps[len(swaps)-1].Timestamp
	}

	for _, swap := range swaps {
		if swap.Type == "buy" {
			cumulativeSold += swap.AmountOut
			if thresholdTokens > 0 && cumulativeSold >= thresholdTokens {
				earlyThresholdTS = swap.Timestamp
				break
			}
		}
	}

	type aggregate struct {
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

	walletAgg := map[string]*aggregate{}
	cumulativeSold = 0
	var totalPreMigrationUSD float64
	var sniperBuyUSD float64

	for _, swap := range swaps {
		if swap.Timestamp > launch.MigratedAt && launch.MigratedAt != 0 {
			continue
		}
		preMigrationSwaps = append(preMigrationSwaps, swap)

		agg := walletAgg[swap.User]
		if agg == nil {
			agg = &aggregate{}
			walletAgg[swap.User] = agg
		}
		agg.all = append(agg.all, swap)

		valueUSD := metrics.USDFromWAD(swap.SwapValueUSD)
		totalPreMigrationUSD += valueUSD

		if swap.Type == "buy" {
			before := cumulativeSold
			cumulativeSold += swap.AmountOut
			agg.buys = append(agg.buys, swap)
			agg.usdSpent += valueUSD
			agg.tokensBought += swap.AmountOut
			if agg.firstBuyTS == 0 || swap.Timestamp < agg.firstBuyTS {
				agg.firstBuyTS = swap.Timestamp
				if launch.TotalTokensSold > 0 {
					agg.entrySupplyPct = (float64(before) / float64(launch.TotalTokensSold)) * 100
				}
			}
		}

		if swap.Type == "sell" {
			agg.sells = append(agg.sells, swap)
			agg.usdReceived += valueUSD
			agg.tokensSold += swap.AmountIn
			if swap.Timestamp > agg.lastSellTS {
				agg.lastSellTS = swap.Timestamp
			}
		}
	}

	wallets := make([]models.WalletLaunchStats, 0, len(walletAgg))
	walletsByAddress := make(map[string]models.WalletLaunchStats, len(walletAgg))

	for wallet, agg := range walletAgg {
		if len(agg.buys) == 0 {
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
			Swaps:             agg.all,
			PreMigrationBuys:  agg.buys,
			PreMigrationSells: agg.sells,
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
		Launch:               launch,
		EarlyThresholdTS:     earlyThresholdTS,
		Wallets:              wallets,
		WalletsByAddress:     walletsByAddress,
		PreMigrationSwaps:    preMigrationSwaps,
		SniperPressureScore:  metrics.PressureScore(sniperBuyUSD, totalPreMigrationUSD),
		TotalSnipedUSD:       totalSnipedUSD(wallets),
		TotalPreMigrationUSD: totalPreMigrationUSD,
		SniperCount:          sniperCount,
		BelieverCount:        believerCount,
		BuyerCount:           len(wallets),
		BelieverLossUSD:      metrics.BelieverLossUSD(wallets),
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
