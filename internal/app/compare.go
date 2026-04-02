package app

import (
	"strings"

	"doppler-sniper/internal/metrics"
	"doppler-sniper/internal/models"
)

func NormalizeCurveType(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "multicurve"):
		return "multicurve"
	case lower == "v3":
		return "v3"
	default:
		return "standard-v4"
	}
}

func buildComparisons(grouped map[string][]models.LaunchAnalysis) []models.CurveComparison {
	order := []string{"multicurve", "standard-v4", "v3"}
	comparisons := make([]models.CurveComparison, 0, len(order))
	for _, curve := range order {
		analyses := grouped[curve]
		var pressures, extracted, sniperCounts, believerLoss []float64
		for _, analysis := range analyses {
			pressures = append(pressures, analysis.SniperPressureScore)
			extracted = append(extracted, analysis.TotalSnipedUSD)
			sniperCounts = append(sniperCounts, float64(analysis.SniperCount))
			believerLoss = append(believerLoss, analysis.BelieverLossUSD)
		}
		comparisons = append(comparisons, models.CurveComparison{
			CurveType:            curve,
			LaunchCount:          len(analyses),
			MedianPressureScore:  metrics.Median(pressures),
			MedianExtractedUSD:   metrics.Median(extracted),
			MedianSniperCount:    metrics.Median(sniperCounts),
			MedianBelieverLossUS: metrics.Median(believerLoss),
		})
	}
	return comparisons
}
