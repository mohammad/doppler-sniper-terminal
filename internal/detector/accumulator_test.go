package detector

import (
	"testing"

	"doppler-sniper/internal/models"
)

func TestLaunchAccumulatorMatchesAnalyzeLaunch(t *testing.T) {
	launch := models.Launch{
		Address:         "0xpool",
		ChainID:         8453,
		Type:            "zora",
		TotalTokensSold: 1_000,
	}
	swaps := []models.Swap{
		{TxHash: "0x1", Pool: "0xpool", ChainID: 8453, User: "0xa", Type: "buy", AmountOut: 200, SwapValueUSD: 1_000_000_000_000_000_000, Timestamp: 10},
		{TxHash: "0x2", Pool: "0xpool", ChainID: 8453, User: "0xa", Type: "sell", AmountIn: 120, SwapValueUSD: 1_500_000_000_000_000_000, Timestamp: 15},
		{TxHash: "0x3", Pool: "0xpool", ChainID: 8453, User: "0xb", Type: "buy", AmountOut: 150, SwapValueUSD: 800_000_000_000_000_000, Timestamp: 20},
		{TxHash: "0x4", Pool: "0xpool", ChainID: 8453, User: "0xb", Type: "sell", AmountIn: 20, SwapValueUSD: 100_000_000_000_000_000, Timestamp: 30},
		{TxHash: "0x5", Pool: "0xpool", ChainID: 8453, User: "0xc", Type: "buy", AmountOut: 80, SwapValueUSD: 200_000_000_000_000_000, Timestamp: 35},
	}

	expected := AnalyzeLaunch(launch, append([]models.Swap(nil), swaps...))

	acc := NewLaunchAccumulator(launch, AccumulatorOptions{
		MaxRecentSwaps: len(swaps),
		MaxWalletSwaps: len(swaps),
	})
	acc.Consume(swaps[:2])
	acc.Consume(swaps[2:])
	actual := acc.Finalize()

	if actual.EarlyThresholdTS != expected.EarlyThresholdTS {
		t.Fatalf("expected early threshold %d, got %d", expected.EarlyThresholdTS, actual.EarlyThresholdTS)
	}
	if actual.SniperCount != expected.SniperCount {
		t.Fatalf("expected sniper count %d, got %d", expected.SniperCount, actual.SniperCount)
	}
	if actual.BelieverCount != expected.BelieverCount {
		t.Fatalf("expected believer count %d, got %d", expected.BelieverCount, actual.BelieverCount)
	}
	if actual.BuyerCount != expected.BuyerCount {
		t.Fatalf("expected buyer count %d, got %d", expected.BuyerCount, actual.BuyerCount)
	}
	if actual.TotalPreMigrationUSD != expected.TotalPreMigrationUSD {
		t.Fatalf("expected total pre-migration usd %f, got %f", expected.TotalPreMigrationUSD, actual.TotalPreMigrationUSD)
	}
	if actual.SniperPressureScore != expected.SniperPressureScore {
		t.Fatalf("expected pressure score %f, got %f", expected.SniperPressureScore, actual.SniperPressureScore)
	}
	if actual.TotalSnipedUSD != expected.TotalSnipedUSD {
		t.Fatalf("expected total sniped usd %f, got %f", expected.TotalSnipedUSD, actual.TotalSnipedUSD)
	}
	if len(actual.Wallets) != len(expected.Wallets) {
		t.Fatalf("expected %d wallets, got %d", len(expected.Wallets), len(actual.Wallets))
	}
	for i := range expected.Wallets {
		if actual.Wallets[i].Wallet != expected.Wallets[i].Wallet {
			t.Fatalf("expected wallet %s at index %d, got %s", expected.Wallets[i].Wallet, i, actual.Wallets[i].Wallet)
		}
		if actual.Wallets[i].Classification != expected.Wallets[i].Classification {
			t.Fatalf("expected classification %s for %s, got %s", expected.Wallets[i].Classification, expected.Wallets[i].Wallet, actual.Wallets[i].Classification)
		}
		if actual.Wallets[i].ProfitUSD != expected.Wallets[i].ProfitUSD {
			t.Fatalf("expected profit %f for %s, got %f", expected.Wallets[i].ProfitUSD, expected.Wallets[i].Wallet, actual.Wallets[i].ProfitUSD)
		}
	}
}

