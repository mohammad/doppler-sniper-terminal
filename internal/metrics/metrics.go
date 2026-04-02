package metrics

import (
	"math"
	"sort"

	"doppler-sniper/internal/models"
)

const wad = 1_000_000_000_000_000_000.0

func USDFromWAD(v int64) float64 {
	return float64(v) / wad
}

func PressureScore(sniperBuyUSD, totalPreMigrationUSD float64) float64 {
	if totalPreMigrationUSD <= 0 {
		return 0
	}
	return (sniperBuyUSD / totalPreMigrationUSD) * 100
}

func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}

func BelieverLossUSD(wallets []models.WalletLaunchStats) float64 {
	var total float64
	for _, wallet := range wallets {
		if wallet.Classification == models.ClassificationBeliever && wallet.ProfitUSD < 0 {
			total += math.Abs(wallet.ProfitUSD)
		}
	}
	return total
}
