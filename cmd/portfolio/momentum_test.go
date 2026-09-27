package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"portfolio/marketdata"
)

func TestRunMomentum_SelectsTopThreeAndReplacesOtherHoldings(t *testing.T) {
	asOf := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	holdingsPath := writeInputFile(t, "holdings.json", `{
  "positions": [
    {"symbol":"TSLA","lots":[{"id":"tsla-lot","quantity":"1","unitCost":"50","acquiredAt":"2025-01-10T15:30:00Z"}]}
  ]
}`)
	universePath := writeInputFile(t, "universe.csv", "symbol\nFLAT\nSLOW\nFAST\nMID\n")
	quoteClient := momentumFixtureQuoteClient(t)
	var output strings.Builder

	err := runMomentum(context.Background(), holdingsPath, settingsPath, universePath, 3, asOf, quoteClient, &output)
	if err != nil {
		t.Fatalf("runMomentum() error = %v", err)
	}
	if !reflect.DeepEqual(quoteClient.historyRequests, []string{"FLAT", "SLOW", "FAST", "MID"}) {
		t.Fatalf("history requests = %#v, want CSV order", quoteClient.historyRequests)
	}
	if !reflect.DeepEqual(quoteClient.requestedSymbols, []string{"FAST", "MID", "SLOW", "TSLA"}) {
		t.Fatalf("latest quote symbols = %#v, want selected assets plus current holding", quoteClient.requestedSymbols)
	}

	var report struct {
		Mode   string `json:"mode"`
		Scores []struct {
			Rank     int    `json:"rank"`
			Symbol   string `json:"symbol"`
			Selected bool   `json:"selected"`
		} `json:"momentumScores"`
		Trades []struct {
			Symbol     string `json:"symbol"`
			Side       string `json:"side"`
			GrossValue string `json:"grossValue"`
		} `json:"trades"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("decode momentum report: %v", err)
	}
	if report.Mode != "momentum_dry_run" {
		t.Fatalf("Mode = %q, want momentum_dry_run", report.Mode)
	}
	wantRanking := []string{"FAST", "MID", "SLOW", "FLAT"}
	if len(report.Scores) != len(wantRanking) {
		t.Fatalf("got %d scores, want %d", len(report.Scores), len(wantRanking))
	}
	for index, score := range report.Scores {
		if score.Rank != index+1 || score.Symbol != wantRanking[index] || score.Selected != (index < 3) {
			t.Errorf("score[%d] = %#v, want rank %d symbol %s selected=%t", index, score, index+1, wantRanking[index], index < 3)
		}
	}
	if len(report.Trades) != 4 {
		t.Fatalf("got %d trades, want 3 buys and one liquidation", len(report.Trades))
	}
	buyValues := make([]string, 0, 3)
	sawLiquidation := false
	for _, trade := range report.Trades {
		if trade.Side == "BUY" {
			buyValues = append(buyValues, trade.GrossValue)
		}
		if trade.Symbol == "TSLA" && trade.Side == "SELL" {
			sawLiquidation = true
		}
	}
	if len(buyValues) != 3 || buyValues[0] != buyValues[1] || buyValues[1] != buyValues[2] {
		t.Fatalf("three selected assets were not purchased at equal values: %#v", buyValues)
	}
	if !sawLiquidation {
		t.Fatal("old TSLA holding was not sold")
	}
}

func TestRunMomentum_DoesNotWritePartialReportOnHistoryFailure(t *testing.T) {
	asOf := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	holdingsPath := writeInputFile(t, "holdings.json", `{"positions":[]}`)
	universePath := writeInputFile(t, "universe.csv", "symbol\nAAA\nBBB\nCCC\n")
	quoteClient := &fakeQuoteClient{historyErr: errors.New("history unavailable")}
	var output strings.Builder

	err := runMomentum(context.Background(), holdingsPath, settingsPath, universePath, 3, asOf, quoteClient, &output)
	if err == nil || !strings.Contains(err.Error(), "history unavailable") {
		t.Fatalf("runMomentum() error = %v, want history error", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no partial report", output.String())
	}
}

func TestRunCLI_MomentumMode(t *testing.T) {
	asOf := time.Now().UTC()
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	holdingsPath := writeInputFile(t, "holdings.json", `{"positions":[]}`)
	universePath := writeInputFile(t, "universe.csv", "symbol\nAAA\nBBB\nCCC\n")
	quoteClient := momentumFixtureQuoteClientForSymbols(t, []string{"AAA", "BBB", "CCC"}, asOf)
	var output strings.Builder
	var errorOutput strings.Builder

	exitCode := runCLI([]string{"-mode", "momentum", "-config", settingsPath, "-portfolio", holdingsPath, "-universe", universePath, "-top", "3"}, quoteClient, &output, &errorOutput)
	if exitCode != 0 {
		t.Fatalf("runCLI(momentum) exit code = %d, stderr=%q", exitCode, errorOutput.String())
	}
	if !strings.Contains(output.String(), `"mode": "momentum_dry_run"`) {
		t.Fatalf("output is not a momentum report: %s", output.String())
	}
}

func TestRunMomentumRejectsFewerCandidatesThanTopCount(t *testing.T) {
	asOf := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	holdingsPath := writeInputFile(t, "holdings.json", `{"positions":[]}`)
	universePath := writeInputFile(t, "universe.csv", "symbol\nAAA\nBBB\n")
	var output strings.Builder
	err := runMomentum(context.Background(), holdingsPath, settingsPath, universePath, 3, asOf, &fakeQuoteClient{}, &output)
	if err == nil || !strings.Contains(err.Error(), "need at least 3") {
		t.Fatalf("runMomentum() error = %v, want insufficient-universe error", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func momentumFixtureQuoteClient(t *testing.T) *fakeQuoteClient {
	t.Helper()
	return momentumFixtureQuoteClientForSymbols(t, []string{"FLAT", "SLOW", "FAST", "MID"}, time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC))
}

func momentumFixtureQuoteClientForSymbols(t *testing.T, symbols []string, asOf time.Time) *fakeQuoteClient {
	t.Helper()
	steps := map[string]string{"FLAT": "0", "SLOW": "1", "MID": "2", "FAST": "4", "AAA": "1", "BBB": "2", "CCC": "3"}
	client := &fakeQuoteClient{histories: make(map[string]marketdata.MonthlyHistory)}
	for _, symbol := range symbols {
		client.histories[symbol] = marketdata.MonthlyHistory{
			Symbol:   symbol,
			Currency: "USD",
			Bars:     makeMomentumBars(t, asOf, "100", steps[symbol]),
		}
	}
	quoteSymbols := append([]string(nil), symbols...)
	quoteSymbols = append(quoteSymbols, "TSLA")
	for _, symbol := range quoteSymbols {
		client.quotes = append(client.quotes, mustQuote(t, symbol, "100"))
	}
	return client
}

func makeMomentumBars(t *testing.T, asOf time.Time, startValue, increment string) []marketdata.MonthlyBar {
	t.Helper()
	monthStart := time.Date(asOf.UTC().Year(), asOf.UTC().Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -20, 0)
	start := decimal.RequireFromString(startValue)
	step := decimal.RequireFromString(increment)
	bars := make([]marketdata.MonthlyBar, 0, 21)
	for index := 0; index < 21; index++ {
		closePrice := start.Add(step.Mul(decimal.NewFromInt(int64(index))))
		bars = append(bars, marketdata.MonthlyBar{
			Time:  monthStart.AddDate(0, index, 0),
			Open:  closePrice,
			High:  closePrice.Add(decimal.NewFromInt(1)),
			Low:   closePrice.Sub(decimal.NewFromInt(1)),
			Close: closePrice,
		})
	}
	return bars
}

func writeMomentumInput(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
