package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"portfolio"
	"portfolio/marketdata"
)

func TestRun_WritesDryRunReport(t *testing.T) {
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	portfolioPath := writeInputFile(t, "portfolio.json", testPortfolioJSON)
	quoteClient := &fakeQuoteClient{quotes: []portfolio.Stock{
		mustQuote(t, "AAPL", "500"),
		mustQuote(t, "META", "500"),
	}}
	var output strings.Builder

	err := run(context.Background(), portfolioPath, settingsPath, quoteClient, &output)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !reflect.DeepEqual(quoteClient.requestedSymbols, []string{"AAPL", "META"}) {
		t.Fatalf("requested symbols = %#v, want [AAPL META]", quoteClient.requestedSymbols)
	}

	var report struct {
		Mode                   string `json:"mode"`
		Currency               string `json:"currency"`
		PortfolioValue         string `json:"portfolioValue"`
		AdditionalCashRequired string `json:"additionalCashRequired"`
		EstimatedTaxTotal      string `json:"estimatedTaxTotal"`
		Quotes                 []struct {
			Symbol string `json:"symbol"`
			Price  string `json:"price"`
			AsOf   string `json:"priceUpdatedAt"`
		} `json:"quotes"`
		Trades []struct {
			Symbol       string `json:"symbol"`
			Side         string `json:"side"`
			Quantity     string `json:"quantity"`
			Commission   string `json:"commission"`
			EstimatedTax string `json:"estimatedTax"`
		} `json:"trades"`
	}
	if err := json.Unmarshal([]byte(output.String()), &report); err != nil {
		t.Fatalf("decode report JSON: %v", err)
	}
	if report.Mode != "dry_run" || report.Currency != "USD" || report.PortfolioValue != "1000" {
		t.Fatalf("report header fields are unexpected: %#v", report)
	}
	if report.AdditionalCashRequired != "4.83" || report.EstimatedTaxTotal != "2.85" {
		t.Fatalf("report totals are unexpected: %#v", report)
	}
	if len(report.Quotes) != 2 || report.Quotes[0].Price != "500" || report.Quotes[0].AsOf == "" {
		t.Fatalf("report quotes are incomplete: %#v", report.Quotes)
	}
	if len(report.Trades) != 2 || report.Trades[0].Symbol != "AAPL" || report.Trades[0].Side != "BUY" || report.Trades[1].Symbol != "META" || report.Trades[1].Side != "SELL" {
		t.Fatalf("report trades are unexpected: %#v", report.Trades)
	}
}

func TestRun_PropagatesQuoteFailureWithoutWritingReport(t *testing.T) {
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	portfolioPath := writeInputFile(t, "portfolio.json", testPortfolioJSON)
	quoteClient := &fakeQuoteClient{err: errors.New("Yahoo unavailable")}
	var output strings.Builder

	err := run(context.Background(), portfolioPath, settingsPath, quoteClient, &output)
	if err == nil || !strings.Contains(err.Error(), "Yahoo unavailable") {
		t.Fatalf("run() error = %v, want quote failure", err)
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want no partial report", output.String())
	}
}

func TestRunCLI_ParsesFlagsAndRunsSimulation(t *testing.T) {
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	portfolioPath := writeInputFile(t, "portfolio.json", testPortfolioJSON)
	quoteClient := &fakeQuoteClient{quotes: []portfolio.Stock{mustQuote(t, "AAPL", "500"), mustQuote(t, "META", "500")}}
	var output strings.Builder
	var errorOutput strings.Builder

	exitCode := runCLI([]string{"-config", settingsPath, "-portfolio", portfolioPath}, quoteClient, &output, &errorOutput)
	if exitCode != 0 {
		t.Fatalf("runCLI() exit code = %d, want 0; stderr: %s", exitCode, errorOutput.String())
	}
	if !strings.Contains(output.String(), `"mode": "dry_run"`) {
		t.Fatalf("output does not contain a dry-run report: %s", output.String())
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errorOutput.String())
	}
}

func TestRunCLI_ReportsFlagAndRuntimeErrors(t *testing.T) {
	portfolioPath := writeInputFile(t, "portfolio.json", testPortfolioJSON)
	tests := []struct {
		name             string
		args             []string
		quoteClient      quoteSource
		wantExitCode     int
		wantErrorMessage string
	}{
		{
			name:             "unknown flag",
			args:             []string{"-unknown"},
			quoteClient:      &fakeQuoteClient{},
			wantExitCode:     2,
			wantErrorMessage: "flag provided but not defined",
		},
		{
			name:             "settings file error",
			args:             []string{"-config", filepath.Join(t.TempDir(), "missing.json"), "-portfolio", portfolioPath},
			quoteClient:      &fakeQuoteClient{},
			wantExitCode:     1,
			wantErrorMessage: "load trading settings",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			var errorOutput strings.Builder
			exitCode := runCLI(test.args, test.quoteClient, &output, &errorOutput)
			if exitCode != test.wantExitCode {
				t.Fatalf("runCLI() exit code = %d, want %d", exitCode, test.wantExitCode)
			}
			if !strings.Contains(errorOutput.String(), test.wantErrorMessage) {
				t.Fatalf("stderr = %q, want it to contain %q", errorOutput.String(), test.wantErrorMessage)
			}
			if output.Len() != 0 {
				t.Fatalf("stdout = %q, want empty on error", output.String())
			}
		})
	}
}

func TestRunRejectsNilDependencies(t *testing.T) {
	settingsPath := writeInputFile(t, "settings.json", testSettingsJSON)
	portfolioPath := writeInputFile(t, "portfolio.json", testPortfolioJSON)
	tests := []struct {
		name        string
		quoteClient quoteSource
		output      io.Writer
	}{
		{name: "nil quote client", output: &strings.Builder{}},
		{name: "nil output", quoteClient: &fakeQuoteClient{}, output: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := run(context.Background(), portfolioPath, settingsPath, test.quoteClient, test.output); err == nil {
				t.Fatal("run() error = nil, want an error")
			}
		})
	}
}

type fakeQuoteClient struct {
	quotes           []portfolio.Stock
	err              error
	requestedSymbols []string
	histories        map[string]marketdata.MonthlyHistory
	historyErr       error
	historyRequests  []string
}

func (f *fakeQuoteClient) Quotes(_ context.Context, symbols []string) ([]portfolio.Stock, error) {
	f.requestedSymbols = append([]string(nil), symbols...)
	if f.err != nil {
		return nil, f.err
	}
	return append([]portfolio.Stock(nil), f.quotes...), nil
}

func (f *fakeQuoteClient) MonthlyHistory(_ context.Context, symbol string) (marketdata.MonthlyHistory, error) {
	f.historyRequests = append(f.historyRequests, symbol)
	if f.historyErr != nil {
		return marketdata.MonthlyHistory{}, f.historyErr
	}
	history, exists := f.histories[symbol]
	if !exists {
		return marketdata.MonthlyHistory{}, errors.New("missing fake history for " + symbol)
	}
	return history, nil
}

func writeInputFile(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func mustQuote(t *testing.T, symbol, price string) portfolio.Stock {
	t.Helper()
	quotePrice, err := decimal.NewFromString(price)
	if err != nil {
		t.Fatalf("parse quote price: %v", err)
	}
	quote, err := portfolio.NewStock(symbol, quotePrice, time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewStock(): %v", err)
	}
	return quote
}

const testSettingsJSON = `{
  "currency": "USD",
  "commission": {"type": "fixed_per_order", "amount": "0.99"},
  "tax_estimate": {"method": "rate_on_positive_realized_gain_after_sell_commission", "rate": "0.15"}
}`

const testPortfolioJSON = `{
  "positions": [
    {"symbol": "META", "lots": [{"id": "meta-1", "quantity": "1", "unitCost": "400", "acquiredAt": "2025-01-10T15:30:00Z"}]},
    {"symbol": "AAPL", "lots": [{"id": "aapl-1", "quantity": "1", "unitCost": "200", "acquiredAt": "2025-01-10T15:30:00Z"}]}
  ],
  "allocations": [
    {"symbol": "META", "targetWeight": "0.4"},
    {"symbol": "AAPL", "targetWeight": "0.6"}
  ]
}`
