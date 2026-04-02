package models

import "time"

type Launch struct {
	Address         string
	ChainID         int
	Asset           string
	Type            string
	CreatedAt       int64
	MigratedAt      int64
	TotalTokensSold int64
	Symbol          string
	Name            string
	Image           string
}

type Swap struct {
	TxHash       string
	Pool         string
	ChainID      int
	User         string
	Type         string
	AmountIn     int64
	AmountOut    int64
	SwapValueUSD int64
	Timestamp    int64
}

type OHLCBucket struct {
	MinuteID  int64
	Open      int64
	Close     int64
	High      int64
	Low       int64
	VolumeUSD int64
	Count     int
}

type Classification string

const (
	ClassificationSniper   Classification = "SNIPER"
	ClassificationBeliever Classification = "BELIEVER"
	ClassificationMixed    Classification = "MIXED"
	ClassificationLate     Classification = "LATE_BUYER"
)

type WalletLaunchStats struct {
	Wallet            string
	Classification    Classification
	FirstBuyTS        int64
	LastSellTS        int64
	EntrySupplyPct    float64
	USDSpent          float64
	USDReceived       float64
	ProfitUSD         float64
	TokensBought      int64
	TokensSold        int64
	SellRatio         float64
	Duration          time.Duration
	Swaps             []Swap
	PreMigrationBuys  []Swap
	PreMigrationSells []Swap
}

type LaunchAnalysis struct {
	Launch               Launch
	EarlyThresholdTS     int64
	Wallets              []WalletLaunchStats
	WalletsByAddress     map[string]WalletLaunchStats
	PreMigrationSwaps    []Swap
	SniperPressureScore  float64
	TotalSnipedUSD       float64
	TotalPreMigrationUSD float64
	SniperCount          int
	BelieverCount        int
	BuyerCount           int
	BelieverLossUSD      float64
}

type LaunchFilter struct {
	ChainIDs  []int
	CurveType string
	DateRange string
	Limit     int
}

type LeaderboardEntry struct {
	Wallet            string
	TotalProfitUSD    float64
	LaunchesSniped    int
	Chains            []int
	AvgEntrySupplyPct float64
	MostRecentSnipeTS int64
}

type CurveComparison struct {
	CurveType            string
	LaunchCount          int
	MedianPressureScore  float64
	MedianExtractedUSD   float64
	MedianSniperCount    float64
	MedianBelieverLossUS float64
	PressureScores       []float64
	ScatterPoints        []ScatterPoint
}

type ScatterPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type PricePoint struct {
	X int     `json:"x"`
	Y float64 `json:"y"`
}

type VolumePoint struct {
	Label       string  `json:"label"`
	SniperUSD   float64 `json:"sniperUsd"`
	BelieverUSD float64 `json:"believerUsd"`
}
