package portfolio_test

import (
	"testing"

	"portfolio"
)

func TestPortfolio_Rebalance_TargetAllocation(t *testing.T) {
	metaStock := mustStock(t, "META", "500")
	aaplStock := mustStock(t, "AAPL", "500")
	metaPosition := mustPositionWithLots(t, metaStock, mustTaxLot(t, "meta-lot", "1", "400", 1))
	aaplPosition := mustPosition(t, aaplStock, "1")
	portfolioValue := mustPortfolio(t,
		[]portfolio.Position{metaPosition, aaplPosition},
		[]portfolio.Allocation{mustAllocation(t, "META", "0.4"), mustAllocation(t, "AAPL", "0.6")},
	)

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{aaplStock, metaStock}, mustDecimal(t, "0.99"), mustDecimal(t, "0.15"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}

	assertDecimal(t, "PortfolioValue", plan.PortfolioValue(), "1000")
	assertDecimal(t, "BuyValue", plan.BuyValue(), "100")
	assertDecimal(t, "SellValue", plan.SellValue(), "100")
	assertDecimal(t, "CommissionTotal", plan.CommissionTotal(), "1.98")
	assertDecimal(t, "EstimatedTaxTotal", plan.EstimatedTaxTotal(), "2.85")
	assertDecimal(t, "NetCashFlow", plan.NetCashFlow(), "-4.83")
	assertDecimal(t, "AdditionalCashRequired", plan.AdditionalCashRequired(), "4.83")
	assertDecimal(t, "RemainingCash", plan.RemainingCash(), "0")
	assertDecimal(t, "RoundingResidual", plan.RoundingResidual(), "0")

	trades := plan.Trades()
	if len(trades) != 2 {
		t.Fatalf("Trades() length = %d, want 2", len(trades))
	}
	assertTrade(t, trades[0], "AAPL", portfolio.TradeSideBuy, "0.2", "500", "100", "0.99", "0", "0", "-100.99")
	assertTrade(t, trades[1], "META", portfolio.TradeSideSell, "0.2", "500", "100", "0.99", "80", "20", "96.16")
	assertDecimal(t, "META taxable gain", trades[1].TaxableGain(), "19.01")
	assertDecimal(t, "META estimated tax", trades[1].EstimatedTax(), "2.85")

	trades[0] = portfolio.RebalanceTrade{}
	if got := plan.Trades()[0].Symbol(); got != "AAPL" {
		t.Fatalf("Trades() exposed internal slice, first symbol = %q", got)
	}
}

func TestPortfolio_Rebalance_AlreadyBalanced(t *testing.T) {
	metaStock := mustStock(t, "META", "100")
	aaplStock := mustStock(t, "AAPL", "100")
	portfolioValue := mustPortfolio(t,
		[]portfolio.Position{mustPosition(t, metaStock, "4"), mustPosition(t, aaplStock, "6")},
		[]portfolio.Allocation{mustAllocation(t, "META", "0.4"), mustAllocation(t, "AAPL", "0.6")},
	)

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{metaStock, aaplStock}, mustDecimal(t, "0.99"), mustDecimal(t, "0.15"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}
	if len(plan.Trades()) != 0 {
		t.Fatalf("Trades() length = %d, want 0", len(plan.Trades()))
	}
	assertDecimal(t, "PortfolioValue", plan.PortfolioValue(), "1000")
	assertDecimal(t, "RoundingResidual", plan.RoundingResidual(), "0")
	assertDecimal(t, "CommissionTotal", plan.CommissionTotal(), "0")
}

func TestPortfolio_Rebalance_UsesLatestQuotes(t *testing.T) {
	storedMetaQuote := mustStock(t, "META", "100")
	latestMetaQuote := mustStock(t, "META", "200")
	latestAaplQuote := mustStock(t, "AAPL", "100")
	metaPosition := mustPositionWithLots(t, storedMetaQuote, mustTaxLot(t, "meta-lot", "1", "50", 1))
	portfolioValue := mustPortfolio(t,
		[]portfolio.Position{metaPosition},
		[]portfolio.Allocation{mustAllocation(t, "META", "0.5"), mustAllocation(t, "AAPL", "0.5")},
	)

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{latestMetaQuote, latestAaplQuote}, mustDecimal(t, "0"), mustDecimal(t, "0"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}
	if len(plan.Trades()) != 2 {
		t.Fatalf("Trades() length = %d, want 2", len(plan.Trades()))
	}
	assertTrade(t, plan.Trades()[0], "AAPL", portfolio.TradeSideBuy, "1", "100", "100", "0", "0", "0", "-100")
	assertTrade(t, plan.Trades()[1], "META", portfolio.TradeSideSell, "0.5", "200", "100", "0", "25", "75", "100")
	assertDecimal(t, "PortfolioValue", plan.PortfolioValue(), "200")
}

func TestPortfolio_Rebalance_SellsUnallocatedAndBuysTarget(t *testing.T) {
	metaStock := mustStock(t, "META", "100")
	aaplStock := mustStock(t, "AAPL", "50")
	metaPosition := mustPositionWithLots(t, metaStock, mustTaxLot(t, "meta-lot", "1", "80", 1))
	portfolioValue := mustPortfolio(t, []portfolio.Position{metaPosition}, []portfolio.Allocation{mustAllocation(t, "AAPL", "1")})

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{metaStock, aaplStock}, mustDecimal(t, "1"), mustDecimal(t, "0.1"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}
	if len(plan.Trades()) != 2 {
		t.Fatalf("Trades() length = %d, want 2", len(plan.Trades()))
	}
	assertTrade(t, plan.Trades()[0], "AAPL", portfolio.TradeSideBuy, "2", "50", "100", "1", "0", "0", "-101")
	assertTrade(t, plan.Trades()[1], "META", portfolio.TradeSideSell, "1", "100", "100", "1", "80", "20", "97.1")
	assertDecimal(t, "META estimated tax", plan.Trades()[1].EstimatedTax(), "1.9")
	assertDecimal(t, "AdditionalCashRequired", plan.AdditionalCashRequired(), "3.9")
}

func TestPortfolio_Rebalance_RoundsShareQuantityDown(t *testing.T) {
	aaaStock := mustStock(t, "AAA", "3")
	bbbStock := mustStock(t, "BBB", "2")
	positions := []portfolio.Position{
		mustPositionWithLots(t, aaaStock, mustTaxLot(t, "aaa-lot", "1", "1", 1)),
		mustPosition(t, bbbStock, "1"),
	}
	portfolioValue := mustPortfolio(t, positions, []portfolio.Allocation{mustAllocation(t, "AAA", "0.4"), mustAllocation(t, "BBB", "0.6")})

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{aaaStock, bbbStock}, mustDecimal(t, "0"), mustDecimal(t, "0"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}
	if len(plan.Trades()) != 2 {
		t.Fatalf("Trades() length = %d, want 2", len(plan.Trades()))
	}
	assertDecimal(t, "AAA sell quantity", plan.Trades()[0].Quantity(), "0.33333333")
	assertDecimal(t, "AAA sale value", plan.Trades()[0].GrossValue(), "0.99999999")
	assertDecimal(t, "RoundingResidual", plan.RoundingResidual(), "0.00000001")
}

func TestPortfolio_Rebalance_ReportsCashRemainingFromRounding(t *testing.T) {
	metaStock := mustStock(t, "META", "100")
	aaplStock := mustStock(t, "AAPL", "3")
	metaPosition := mustPositionWithLots(t, metaStock, mustTaxLot(t, "meta-lot", "10", "50", 1))
	portfolioValue := mustPortfolio(t, []portfolio.Position{metaPosition}, []portfolio.Allocation{mustAllocation(t, "AAPL", "1")})

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{metaStock, aaplStock}, mustDecimal(t, "0"), mustDecimal(t, "0"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}
	assertDecimal(t, "NetCashFlow", plan.NetCashFlow(), "0.00000001")
	assertDecimal(t, "RemainingCash", plan.RemainingCash(), "0.00000001")
	assertDecimal(t, "AdditionalCashRequired", plan.AdditionalCashRequired(), "0")
	assertDecimal(t, "RoundingResidual", plan.RoundingResidual(), "0.00000001")
}

func TestPortfolio_Rebalance_RejectsInvalidInputs(t *testing.T) {
	metaStock := mustStock(t, "META", "100")
	aaplStock := mustStock(t, "AAPL", "100")
	positionWithBasis := mustPositionWithLots(t, metaStock, mustTaxLot(t, "meta-lot", "1", "80", 1))
	positionWithoutBasis := mustPosition(t, metaStock, "1")
	targetedPortfolio := mustPortfolio(t,
		[]portfolio.Position{positionWithBasis},
		[]portfolio.Allocation{mustAllocation(t, "META", "0.5"), mustAllocation(t, "AAPL", "0.5")},
	)
	unallocatedPortfolio := mustPortfolio(t, []portfolio.Position{positionWithoutBasis}, []portfolio.Allocation{mustAllocation(t, "AAPL", "1")})

	tests := []struct {
		name           string
		portfolio      portfolio.Portfolio
		quotes         []portfolio.Stock
		commission     string
		taxRate        string
		wantErrorField string
	}{
		{name: "rejects missing held-stock quote", portfolio: targetedPortfolio, quotes: []portfolio.Stock{aaplStock}, commission: "1", taxRate: "0.1", wantErrorField: "quotes"},
		{name: "rejects missing target-stock quote", portfolio: targetedPortfolio, quotes: []portfolio.Stock{metaStock}, commission: "1", taxRate: "0.1", wantErrorField: "quotes"},
		{name: "rejects duplicate quote symbols", portfolio: targetedPortfolio, quotes: []portfolio.Stock{metaStock, metaStock, aaplStock}, commission: "1", taxRate: "0.1", wantErrorField: "quotes"},
		{name: "rejects invalid quote", portfolio: targetedPortfolio, quotes: []portfolio.Stock{{}, aaplStock}, commission: "1", taxRate: "0.1", wantErrorField: "quotes"},
		{name: "rejects sale without acquisition basis", portfolio: unallocatedPortfolio, quotes: []portfolio.Stock{metaStock, aaplStock}, commission: "1", taxRate: "0.1", wantErrorField: "lots"},
		{name: "rejects invalid commission", portfolio: targetedPortfolio, quotes: []portfolio.Stock{metaStock, aaplStock}, commission: "-0.01", taxRate: "0.1", wantErrorField: "commission"},
		{name: "rejects invalid tax rate", portfolio: targetedPortfolio, quotes: []portfolio.Stock{metaStock, aaplStock}, commission: "1", taxRate: "1.01", wantErrorField: "estimatedTaxRate"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.portfolio.Rebalance(test.quotes, mustDecimal(t, test.commission), mustDecimal(t, test.taxRate))
			assertValidationError(t, err, test.wantErrorField)
		})
	}
}

func TestPortfolio_Rebalance_ZeroValuePortfolioReturnsNoOrders(t *testing.T) {
	metaStock := mustStock(t, "META", "100")
	portfolioValue := mustPortfolio(t, nil, []portfolio.Allocation{mustAllocation(t, "META", "1")})

	plan, err := portfolioValue.Rebalance([]portfolio.Stock{metaStock}, mustDecimal(t, "0.99"), mustDecimal(t, "0.15"))
	if err != nil {
		t.Fatalf("Rebalance() error = %v", err)
	}
	if len(plan.Trades()) != 0 {
		t.Fatalf("Trades() length = %d, want 0", len(plan.Trades()))
	}
	assertDecimal(t, "PortfolioValue", plan.PortfolioValue(), "0")
}

func mustPortfolio(t *testing.T, positions []portfolio.Position, allocations []portfolio.Allocation) portfolio.Portfolio {
	t.Helper()
	result, err := portfolio.NewPortfolio(positions, allocations)
	if err != nil {
		t.Fatalf("NewPortfolio(): %v", err)
	}
	return result
}

func assertTrade(t *testing.T, trade portfolio.RebalanceTrade, symbol string, side portfolio.TradeSide, quantity, price, grossValue, commission, costBasis, realizedGain, netCashFlow string) {
	t.Helper()
	if trade.Symbol() != symbol {
		t.Errorf("Symbol() = %q, want %q", trade.Symbol(), symbol)
	}
	if trade.Side() != side {
		t.Errorf("Side() = %q, want %q", trade.Side(), side)
	}
	assertDecimal(t, "Quantity", trade.Quantity(), quantity)
	assertDecimal(t, "Price", trade.Price(), price)
	assertDecimal(t, "GrossValue", trade.GrossValue(), grossValue)
	assertDecimal(t, "Commission", trade.Commission(), commission)
	assertDecimal(t, "CostBasis", trade.CostBasis(), costBasis)
	assertDecimal(t, "RealizedGain", trade.RealizedGain(), realizedGain)
	assertDecimal(t, "NetCashFlow", trade.NetCashFlow(), netCashFlow)
}

func assertDecimal(t *testing.T, name string, got interface{ String() string }, want string) {
	t.Helper()
	if got.String() != want {
		t.Errorf("%s = %q, want %q", name, got.String(), want)
	}
}
