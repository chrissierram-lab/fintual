package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"portfolio/config"
	"portfolio/marketdata"
	"portfolio/momentum"
	"portfolio/universe"
)

func runMomentum(ctx context.Context, holdingsPath, settingsPath, universePath string, topCount int, asOf time.Time, quoteClient quoteSource, output io.Writer) error {
	if quoteClient == nil {
		return fmt.Errorf("quote client must not be nil")
	}
	if output == nil {
		return fmt.Errorf("output writer must not be nil")
	}
	if topCount <= 0 {
		return fmt.Errorf("top count must be positive")
	}
	if asOf.IsZero() {
		return fmt.Errorf("as-of time must be provided")
	}

	settings, err := config.LoadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("load trading settings: %w", err)
	}
	holdings, err := config.LoadHoldingsFile(holdingsPath)
	if err != nil {
		return fmt.Errorf("load current holdings: %w", err)
	}
	candidates, err := universe.LoadCSVFile(universePath)
	if err != nil {
		return fmt.Errorf("load candidate universe: %w", err)
	}
	if len(candidates) < topCount {
		return fmt.Errorf("universe has %d symbols; need at least %d", len(candidates), topCount)
	}

	histories := make([]marketdata.MonthlyHistory, 0, len(candidates))
	for _, symbol := range candidates {
		history, err := quoteClient.MonthlyHistory(ctx, symbol)
		if err != nil {
			return fmt.Errorf("fetch monthly history for %s: %w", symbol, err)
		}
		if !strings.EqualFold(history.Symbol, symbol) {
			return fmt.Errorf("Yahoo returned history for %s while requesting %s", history.Symbol, symbol)
		}
		histories = append(histories, history)
	}

	ranked, err := momentum.Rank(histories, asOf)
	if err != nil {
		return fmt.Errorf("rank candidate momentum: %w", err)
	}
	selected, err := momentum.Top(ranked, topCount)
	if err != nil {
		return fmt.Errorf("select top momentum assets: %w", err)
	}
	allocations, err := momentum.EqualWeightAllocations(selected)
	if err != nil {
		return fmt.Errorf("create equal target allocations: %w", err)
	}

	quoteSymbols := make(map[string]struct{}, len(holdings.PositionSymbols())+len(selected))
	for _, symbol := range holdings.PositionSymbols() {
		quoteSymbols[symbol] = struct{}{}
	}
	for _, score := range selected {
		quoteSymbols[score.Symbol] = struct{}{}
	}
	symbols := make([]string, 0, len(quoteSymbols))
	for symbol := range quoteSymbols {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	latestQuotes, err := quoteClient.Quotes(ctx, symbols)
	if err != nil {
		return fmt.Errorf("fetch latest prices for holdings and selected assets: %w", err)
	}
	currentPortfolio, err := holdings.BuildWithAllocations(latestQuotes, allocations)
	if err != nil {
		return fmt.Errorf("build current holdings with momentum targets: %w", err)
	}
	plan, err := currentPortfolio.Rebalance(latestQuotes, settings.FixedCommissionPerOrder(), settings.EstimatedTaxRate())
	if err != nil {
		return fmt.Errorf("calculate momentum rebalance plan: %w", err)
	}

	selectedSymbols := make(map[string]struct{}, len(selected))
	for _, score := range selected {
		selectedSymbols[score.Symbol] = struct{}{}
	}
	report := makeSimulationReport(settings.Currency(), latestQuotes, plan)
	report.Mode = "momentum_dry_run"
	report.MomentumScores = make([]momentumScoreReport, 0, len(ranked))
	for index, score := range ranked {
		_, isSelected := selectedSymbols[score.Symbol]
		report.MomentumScores = append(report.MomentumScores, momentumScoreReport{
			Rank:          index + 1,
			Symbol:        score.Symbol,
			Score:         score.Value.String(),
			ROC8:          score.ROC8.String(),
			ROC10:         score.ROC10.String(),
			ATR14:         score.ATR14.String(),
			Average14:     score.Average14.String(),
			RelativeATR14: score.RelativeATR14.String(),
			AsOf:          score.AsOf.Format("2006-01-02"),
			Selected:      isSelected,
		})
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write momentum simulation report: %w", err)
	}
	return nil
}
