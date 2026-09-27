package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"portfolio"
	"portfolio/config"
	"portfolio/marketdata"
	"portfolio/quotes/yahoo"
)

type quoteSource interface {
	Quotes(context.Context, []string) ([]portfolio.Stock, error)
	MonthlyHistory(context.Context, string) (marketdata.MonthlyHistory, error)
}

type simulationReport struct {
	Mode                   string                `json:"mode"`
	Currency               string                `json:"currency"`
	Quotes                 []quoteReport         `json:"quotes"`
	PortfolioValue         string                `json:"portfolioValue"`
	Trades                 []tradeReport         `json:"trades"`
	BuyValue               string                `json:"buyValue"`
	SellValue              string                `json:"sellValue"`
	CommissionTotal        string                `json:"commissionTotal"`
	EstimatedTaxTotal      string                `json:"estimatedTaxTotal"`
	NetCashFlow            string                `json:"netCashFlow"`
	AdditionalCashRequired string                `json:"additionalCashRequired"`
	RemainingCash          string                `json:"remainingCash"`
	RoundingResidual       string                `json:"roundingResidual"`
	MomentumScores         []momentumScoreReport `json:"momentumScores,omitempty"`
}

type momentumScoreReport struct {
	Rank          int    `json:"rank"`
	Symbol        string `json:"symbol"`
	Score         string `json:"score"`
	ROC8          string `json:"roc8"`
	ROC10         string `json:"roc10"`
	ATR14         string `json:"atr14"`
	Average14     string `json:"average14"`
	RelativeATR14 string `json:"relativeAtr14"`
	AsOf          string `json:"asOf"`
	Selected      bool   `json:"selected"`
}

type quoteReport struct {
	Symbol         string `json:"symbol"`
	Price          string `json:"price"`
	PriceUpdatedAt string `json:"priceUpdatedAt"`
}

type tradeReport struct {
	Symbol       string `json:"symbol"`
	Side         string `json:"side"`
	Quantity     string `json:"quantity"`
	Price        string `json:"price"`
	GrossValue   string `json:"grossValue"`
	Commission   string `json:"commission"`
	CostBasis    string `json:"costBasis"`
	RealizedGain string `json:"realizedGain"`
	TaxableGain  string `json:"taxableGain"`
	EstimatedTax string `json:"estimatedTax"`
	NetCashFlow  string `json:"netCashFlow"`
}

func main() {
	os.Exit(runCLI(os.Args[1:], yahoo.NewClient(), os.Stdout, os.Stderr))
}

func runCLI(args []string, quoteClient quoteSource, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("portfolio", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	mode := flags.String("mode", "static", "rebalance strategy: static or momentum")
	settingsPath := flags.String("config", "config/config.json", "path to trading-cost settings JSON")
	portfolioPath := flags.String("portfolio", "config/portfolio.example.json", "path to portfolio definition JSON")
	universePath := flags.String("universe", "config/universe.example.csv", "candidate ticker CSV for momentum mode")
	topCount := flags.Int("top", 3, "number of highest momentum assets to target")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	var err error
	switch *mode {
	case "static":
		err = run(ctx, *portfolioPath, *settingsPath, quoteClient, output)
	case "momentum":
		err = runMomentum(ctx, *portfolioPath, *settingsPath, *universePath, *topCount, time.Now().UTC(), quoteClient, output)
	default:
		err = fmt.Errorf("unknown mode %q; expected static or momentum", *mode)
	}
	if err != nil {
		fmt.Fprintln(errorOutput, err)
		return 1
	}
	return 0
}

func run(ctx context.Context, portfolioPath, settingsPath string, quoteClient quoteSource, output io.Writer) error {
	if quoteClient == nil {
		return fmt.Errorf("quote client must not be nil")
	}
	if output == nil {
		return fmt.Errorf("output writer must not be nil")
	}

	settings, err := config.LoadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("load trading settings: %w", err)
	}
	definition, err := config.LoadPortfolioFile(portfolioPath)
	if err != nil {
		return fmt.Errorf("load portfolio definition: %w", err)
	}
	quotes, err := quoteClient.Quotes(ctx, definition.Symbols())
	if err != nil {
		return fmt.Errorf("fetch portfolio quotes: %w", err)
	}
	holdings, err := definition.Build(quotes)
	if err != nil {
		return fmt.Errorf("build portfolio from quotes: %w", err)
	}
	plan, err := holdings.Rebalance(quotes, settings.FixedCommissionPerOrder(), settings.EstimatedTaxRate())
	if err != nil {
		return fmt.Errorf("calculate rebalance plan: %w", err)
	}

	report := makeSimulationReport(settings.Currency(), quotes, plan)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write simulation report: %w", err)
	}
	return nil
}

func makeSimulationReport(currency string, quotes []portfolio.Stock, plan portfolio.RebalancePlan) simulationReport {
	quoteOutput := make([]quoteReport, 0, len(quotes))
	for _, quote := range quotes {
		quoteOutput = append(quoteOutput, quoteReport{
			Symbol:         quote.Symbol(),
			Price:          quote.CurrentPrice().String(),
			PriceUpdatedAt: quote.PriceUpdatedAt().UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	trades := plan.Trades()
	tradeOutput := make([]tradeReport, 0, len(trades))
	for _, trade := range trades {
		tradeOutput = append(tradeOutput, tradeReport{
			Symbol:       trade.Symbol(),
			Side:         string(trade.Side()),
			Quantity:     trade.Quantity().String(),
			Price:        trade.Price().String(),
			GrossValue:   trade.GrossValue().String(),
			Commission:   trade.Commission().String(),
			CostBasis:    trade.CostBasis().String(),
			RealizedGain: trade.RealizedGain().String(),
			TaxableGain:  trade.TaxableGain().String(),
			EstimatedTax: trade.EstimatedTax().String(),
			NetCashFlow:  trade.NetCashFlow().String(),
		})
	}

	return simulationReport{
		Mode:                   "dry_run",
		Currency:               currency,
		Quotes:                 quoteOutput,
		PortfolioValue:         plan.PortfolioValue().String(),
		Trades:                 tradeOutput,
		BuyValue:               plan.BuyValue().String(),
		SellValue:              plan.SellValue().String(),
		CommissionTotal:        plan.CommissionTotal().String(),
		EstimatedTaxTotal:      plan.EstimatedTaxTotal().String(),
		NetCashFlow:            plan.NetCashFlow().String(),
		AdditionalCashRequired: plan.AdditionalCashRequired().String(),
		RemainingCash:          plan.RemainingCash().String(),
		RoundingResidual:       plan.RoundingResidual().String(),
	}
}
