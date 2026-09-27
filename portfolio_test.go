package portfolio_test

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"portfolio"
)

func TestNewStock(t *testing.T) {
	tests := []struct {
		name           string
		symbol         string
		price          string
		updatedAt      time.Time
		wantSymbol     string
		wantErrorField string
	}{
		{
			name:       "preserves exact decimal price and normalizes symbol",
			symbol:     " meta ",
			price:      "123.45000001",
			updatedAt:  time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC),
			wantSymbol: "META",
		},
		{
			name:           "rejects empty symbol",
			symbol:         "  ",
			price:          "10",
			updatedAt:      time.Now(),
			wantErrorField: "symbol",
		},
		{
			name:           "rejects zero price",
			symbol:         "META",
			price:          "0",
			updatedAt:      time.Now(),
			wantErrorField: "currentPrice",
		},
		{
			name:           "rejects negative price",
			symbol:         "META",
			price:          "-1",
			updatedAt:      time.Now(),
			wantErrorField: "currentPrice",
		},
		{
			name:           "rejects missing quote timestamp",
			symbol:         "META",
			price:          "10",
			wantErrorField: "priceUpdatedAt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stock, err := portfolio.NewStock(test.symbol, mustDecimal(t, test.price), test.updatedAt)
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if stock.Symbol() != test.wantSymbol {
				t.Fatalf("Symbol() = %q, want %q", stock.Symbol(), test.wantSymbol)
			}
			if got := stock.CurrentPrice().String(); got != test.price {
				t.Fatalf("CurrentPrice() = %q, want exact value %q", got, test.price)
			}
		})
	}
}

func TestStock_WithCurrentPrice(t *testing.T) {
	original := mustStock(t, "META", "100")
	updatedAt := time.Date(2026, time.September, 28, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		stock          portfolio.Stock
		price          string
		updatedAt      time.Time
		wantPrice      string
		wantErrorField string
	}{
		{name: "returns a copy with the latest exact price", stock: original, price: "123.45000001", updatedAt: updatedAt, wantPrice: "123.45000001"},
		{name: "rejects zero latest price", stock: original, price: "0", updatedAt: updatedAt, wantErrorField: "currentPrice"},
		{name: "rejects negative latest price", stock: original, price: "-1", updatedAt: updatedAt, wantErrorField: "currentPrice"},
		{name: "rejects missing timestamp", stock: original, price: "123.45", wantErrorField: "priceUpdatedAt"},
		{name: "rejects zero-value stock", price: "123.45", updatedAt: updatedAt, wantErrorField: "symbol"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updated, err := test.stock.WithCurrentPrice(mustDecimal(t, test.price), test.updatedAt)
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if got := updated.CurrentPrice().String(); got != test.wantPrice {
				t.Fatalf("CurrentPrice() = %q, want %q", got, test.wantPrice)
			}
			if !updated.PriceUpdatedAt().Equal(test.updatedAt) {
				t.Fatalf("PriceUpdatedAt() = %s, want %s", updated.PriceUpdatedAt(), test.updatedAt)
			}
			if updated.Symbol() != test.stock.Symbol() {
				t.Fatalf("updated symbol = %q, want %q", updated.Symbol(), test.stock.Symbol())
			}
		})
	}

	updated, err := original.WithCurrentPrice(mustDecimal(t, "123.45"), updatedAt)
	if err != nil {
		t.Fatalf("WithCurrentPrice() error = %v", err)
	}
	if got := original.CurrentPrice().String(); got != "100" {
		t.Fatalf("WithCurrentPrice() mutated original price to %q", got)
	}
	if got := updated.CurrentPrice().String(); got != "123.45" {
		t.Fatalf("updated price = %q, want 123.45", got)
	}
}

func TestNewPosition(t *testing.T) {
	stock := mustStock(t, "META", "123.45")
	tests := []struct {
		name            string
		stock           portfolio.Stock
		quantity        string
		wantErrorField  string
		wantExactAmount string
	}{
		{name: "preserves fractional share quantity", stock: stock, quantity: "0.00000001", wantExactAmount: "0.00000001"},
		{name: "rejects zero quantity", stock: stock, quantity: "0", wantErrorField: "quantity"},
		{name: "rejects negative quantity", stock: stock, quantity: "-0.5", wantErrorField: "quantity"},
		{name: "rejects zero-value stock", quantity: "1", wantErrorField: "stock"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			position, err := portfolio.NewPosition(test.stock, mustDecimal(t, test.quantity))
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if got := position.Quantity().String(); got != test.wantExactAmount {
				t.Fatalf("Quantity() = %q, want %q", got, test.wantExactAmount)
			}
		})
	}
}

func TestNewTaxLot(t *testing.T) {
	acquiredAt := time.Date(2025, time.January, 2, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name           string
		id             string
		quantity       string
		unitCost       string
		acquiredAt     time.Time
		wantErrorField string
	}{
		{name: "accepts exact acquisition data", id: "lot-1", quantity: "0.25", unitCost: "101.12345678", acquiredAt: acquiredAt},
		{name: "rejects empty ID", id: " ", quantity: "1", unitCost: "10", acquiredAt: acquiredAt, wantErrorField: "lotID"},
		{name: "rejects zero quantity", id: "lot-1", quantity: "0", unitCost: "10", acquiredAt: acquiredAt, wantErrorField: "quantity"},
		{name: "rejects negative quantity", id: "lot-1", quantity: "-1", unitCost: "10", acquiredAt: acquiredAt, wantErrorField: "quantity"},
		{name: "rejects zero unit cost", id: "lot-1", quantity: "1", unitCost: "0", acquiredAt: acquiredAt, wantErrorField: "unitCost"},
		{name: "rejects negative unit cost", id: "lot-1", quantity: "1", unitCost: "-10", acquiredAt: acquiredAt, wantErrorField: "unitCost"},
		{name: "rejects missing acquisition date", id: "lot-1", quantity: "1", unitCost: "10", wantErrorField: "acquiredAt"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lot, err := portfolio.NewTaxLot(test.id, mustDecimal(t, test.quantity), mustDecimal(t, test.unitCost), test.acquiredAt)
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if lot.ID() != "lot-1" || lot.Quantity().String() != test.quantity || lot.UnitCost().String() != test.unitCost || !lot.AcquiredAt().Equal(test.acquiredAt) {
				t.Fatalf("NewTaxLot() returned unexpected data: %#v", lot)
			}
		})
	}
}

func TestNewPositionWithLots(t *testing.T) {
	stock := mustStock(t, "META", "15")
	firstLot := mustTaxLot(t, "lot-1", "1.25", "10", 1)
	secondLot := mustTaxLot(t, "lot-2", "0.75", "12", 2)

	tests := []struct {
		name           string
		stock          portfolio.Stock
		lots           []portfolio.TaxLot
		wantQuantity   string
		wantErrorField string
	}{
		{name: "derives quantity from acquisition lots", stock: stock, lots: []portfolio.TaxLot{firstLot, secondLot}, wantQuantity: "2"},
		{name: "rejects empty lots", stock: stock, wantErrorField: "lots"},
		{name: "rejects invalid stock", lots: []portfolio.TaxLot{firstLot}, wantErrorField: "stock"},
		{name: "rejects invalid zero-value lot", stock: stock, lots: []portfolio.TaxLot{{}}, wantErrorField: "lots"},
		{name: "rejects duplicate lot IDs", stock: stock, lots: []portfolio.TaxLot{firstLot, firstLot}, wantErrorField: "lots"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			position, err := portfolio.NewPositionWithLots(test.stock, test.lots)
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if got := position.Quantity().String(); got != test.wantQuantity {
				t.Fatalf("Quantity() = %q, want %q", got, test.wantQuantity)
			}
			if position.Stock().Symbol() != "META" {
				t.Fatalf("Stock().Symbol() = %q, want META", position.Stock().Symbol())
			}
		})
	}

	inputLots := []portfolio.TaxLot{firstLot, secondLot}
	position, err := portfolio.NewPositionWithLots(stock, inputLots)
	if err != nil {
		t.Fatalf("NewPositionWithLots() error = %v", err)
	}
	inputLots[0] = portfolio.TaxLot{}
	returnedLots := position.Lots()
	returnedLots[1] = portfolio.TaxLot{}
	if got := position.Lots()[0].ID(); got != "lot-1" {
		t.Fatalf("Lots() exposed internal slice; first ID = %q", got)
	}
}

func TestAllocateSaleFIFO(t *testing.T) {
	stock := mustStock(t, "META", "15")
	oldLot := mustTaxLot(t, "lot-a", "1", "10", 1)
	newLot := mustTaxLot(t, "lot-b", "2", "12", 2)
	position := mustPositionWithLots(t, stock, oldLot, newLot)
	reversedPosition := mustPositionWithLots(t, stock, newLot, oldLot)
	equalDateLotA := mustTaxLotAt(t, "lot-a", "1", "10", time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC))
	equalDateLotB := mustTaxLotAt(t, "lot-b", "1", "20", time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC))
	equalDatePosition := mustPositionWithLots(t, stock, equalDateLotB, equalDateLotA)
	unknownBasisPosition := mustPosition(t, stock, "1")

	tests := []struct {
		name               string
		position           portfolio.Position
		saleQuantity       string
		wantCostBasis      string
		wantGrossProceeds  string
		wantRealizedGain   string
		wantSoldIDs        []string
		wantSoldQuantities []string
		wantRemainingIDs   []string
		wantRemaining      []string
		wantErrorField     string
	}{
		{
			name:               "partially sells the oldest lot",
			position:           position,
			saleQuantity:       "0.5",
			wantCostBasis:      "5",
			wantGrossProceeds:  "7.5",
			wantRealizedGain:   "2.5",
			wantSoldIDs:        []string{"lot-a"},
			wantSoldQuantities: []string{"0.5"},
			wantRemainingIDs:   []string{"lot-a", "lot-b"},
			wantRemaining:      []string{"0.5", "2"},
		},
		{
			name:               "crosses lots in acquisition order regardless of input order",
			position:           reversedPosition,
			saleQuantity:       "1.5",
			wantCostBasis:      "16",
			wantGrossProceeds:  "22.5",
			wantRealizedGain:   "6.5",
			wantSoldIDs:        []string{"lot-a", "lot-b"},
			wantSoldQuantities: []string{"1", "0.5"},
			wantRemainingIDs:   []string{"lot-b"},
			wantRemaining:      []string{"1.5"},
		},
		{
			name:               "uses lot ID as a deterministic tie-breaker",
			position:           equalDatePosition,
			saleQuantity:       "1",
			wantCostBasis:      "10",
			wantGrossProceeds:  "15",
			wantRealizedGain:   "5",
			wantSoldIDs:        []string{"lot-a"},
			wantSoldQuantities: []string{"1"},
			wantRemainingIDs:   []string{"lot-b"},
			wantRemaining:      []string{"1"},
		},
		{
			name:               "preserves precision for fractional shares and unit cost",
			position:           mustPositionWithLots(t, mustStock(t, "META", "0.22345678"), mustTaxLot(t, "tiny", "0.00000003", "0.12345678", 1)),
			saleQuantity:       "0.00000001",
			wantCostBasis:      "0.0000000012345678",
			wantGrossProceeds:  "0.0000000022345678",
			wantRealizedGain:   "0.000000001",
			wantSoldIDs:        []string{"tiny"},
			wantSoldQuantities: []string{"0.00000001"},
			wantRemainingIDs:   []string{"tiny"},
			wantRemaining:      []string{"0.00000002"},
		},
		{name: "rejects a quantity over the position", position: position, saleQuantity: "4", wantErrorField: "quantity"},
		{name: "rejects zero sale quantity", position: position, saleQuantity: "0", wantErrorField: "quantity"},
		{name: "rejects missing cost basis", position: unknownBasisPosition, saleQuantity: "0.5", wantErrorField: "lots"},
		{name: "rejects an invalid position", saleQuantity: "1", wantErrorField: "position"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocation, err := portfolio.AllocateSaleFIFO(test.position, mustDecimal(t, test.saleQuantity))
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if got := allocation.CostBasis().String(); got != test.wantCostBasis {
				t.Errorf("CostBasis() = %q, want %q", got, test.wantCostBasis)
			}
			if got := allocation.GrossProceeds().String(); got != test.wantGrossProceeds {
				t.Errorf("GrossProceeds() = %q, want %q", got, test.wantGrossProceeds)
			}
			if got := allocation.RealizedGain().String(); got != test.wantRealizedGain {
				t.Errorf("RealizedGain() = %q, want %q", got, test.wantRealizedGain)
			}
			assertLotIDsAndQuantities(t, allocation.SoldLots(), test.wantSoldIDs, test.wantSoldQuantities)
			assertLotIDsAndQuantities(t, allocation.RemainingLots(), test.wantRemainingIDs, test.wantRemaining)
			if !allocation.Quantity().Equal(mustDecimal(t, test.saleQuantity)) {
				t.Errorf("Quantity() = %s, want %s", allocation.Quantity(), test.saleQuantity)
			}
		})
	}

	if got := position.Quantity().String(); got != "3" {
		t.Fatalf("AllocateSaleFIFO mutated source position quantity: got %s, want 3", got)
	}
}

func TestNewAllocation(t *testing.T) {
	tests := []struct {
		name           string
		symbol         string
		weight         string
		wantErrorField string
	}{
		{name: "accepts exact fractional weight", symbol: "AAPL", weight: "0.6"},
		{name: "accepts full allocation", symbol: "AAPL", weight: "1"},
		{name: "rejects empty symbol", symbol: " ", weight: "0.4", wantErrorField: "symbol"},
		{name: "rejects zero weight", symbol: "AAPL", weight: "0", wantErrorField: "targetWeight"},
		{name: "rejects weight over one", symbol: "AAPL", weight: "1.0001", wantErrorField: "targetWeight"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocation, err := portfolio.NewAllocation(test.symbol, mustDecimal(t, test.weight))
			assertValidationError(t, err, test.wantErrorField)
			if err == nil && allocation.TargetWeight().String() != test.weight {
				t.Fatalf("TargetWeight() = %q, want %q", allocation.TargetWeight(), test.weight)
			}
		})
	}
}

func TestNewPortfolio(t *testing.T) {
	metaStock := mustStock(t, "META", "500.25")
	aaplStock := mustStock(t, "AAPL", "200.10")
	metaPosition := mustPosition(t, metaStock, "1")
	aaplPosition := mustPosition(t, aaplStock, "2")
	metaAllocation := mustAllocation(t, "META", "0.4")
	aaplAllocation := mustAllocation(t, "AAPL", "0.6")

	tests := []struct {
		name           string
		positions      []portfolio.Position
		allocations    []portfolio.Allocation
		wantErrorField string
	}{
		{
			name:        "accepts weights totaling exactly one",
			positions:   []portfolio.Position{metaPosition, aaplPosition},
			allocations: []portfolio.Allocation{metaAllocation, aaplAllocation},
		},
		{
			name:           "rejects empty allocations",
			positions:      []portfolio.Position{metaPosition},
			wantErrorField: "allocations",
		},
		{
			name:           "rejects a tiny allocation total mismatch",
			allocations:    []portfolio.Allocation{mustAllocation(t, "META", "0.4"), mustAllocation(t, "AAPL", "0.5999999999")},
			wantErrorField: "allocations",
		},
		{
			name:           "rejects duplicate position symbols",
			positions:      []portfolio.Position{metaPosition, mustPosition(t, metaStock, "2")},
			allocations:    []portfolio.Allocation{mustAllocation(t, "META", "1")},
			wantErrorField: "positions",
		},
		{
			name:           "rejects duplicate allocation symbols",
			allocations:    []portfolio.Allocation{mustAllocation(t, "META", "0.4"), mustAllocation(t, " meta ", "0.6")},
			wantErrorField: "allocations",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := portfolio.NewPortfolio(test.positions, test.allocations)
			assertValidationError(t, err, test.wantErrorField)
		})
	}
}

func mustDecimal(t *testing.T, value string) decimal.Decimal {
	t.Helper()
	result, err := decimal.NewFromString(value)
	if err != nil {
		t.Fatalf("parse decimal %q: %v", value, err)
	}
	return result
}

func mustStock(t *testing.T, symbol, price string) portfolio.Stock {
	t.Helper()
	stock, err := portfolio.NewStock(symbol, mustDecimal(t, price), time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("NewStock(): %v", err)
	}
	return stock
}

func mustPosition(t *testing.T, stock portfolio.Stock, quantity string) portfolio.Position {
	t.Helper()
	position, err := portfolio.NewPosition(stock, mustDecimal(t, quantity))
	if err != nil {
		t.Fatalf("NewPosition(): %v", err)
	}
	return position
}

func mustTaxLot(t *testing.T, id, quantity, unitCost string, acquisitionDay int) portfolio.TaxLot {
	t.Helper()
	return mustTaxLotAt(t, id, quantity, unitCost, time.Date(2025, time.January, acquisitionDay, 9, 0, 0, 0, time.UTC))
}

func mustTaxLotAt(t *testing.T, id, quantity, unitCost string, acquiredAt time.Time) portfolio.TaxLot {
	t.Helper()
	lot, err := portfolio.NewTaxLot(id, mustDecimal(t, quantity), mustDecimal(t, unitCost), acquiredAt)
	if err != nil {
		t.Fatalf("NewTaxLot(): %v", err)
	}
	return lot
}

func mustPositionWithLots(t *testing.T, stock portfolio.Stock, lots ...portfolio.TaxLot) portfolio.Position {
	t.Helper()
	position, err := portfolio.NewPositionWithLots(stock, lots)
	if err != nil {
		t.Fatalf("NewPositionWithLots(): %v", err)
	}
	return position
}

func mustAllocation(t *testing.T, symbol, weight string) portfolio.Allocation {
	t.Helper()
	allocation, err := portfolio.NewAllocation(symbol, mustDecimal(t, weight))
	if err != nil {
		t.Fatalf("NewAllocation(): %v", err)
	}
	return allocation
}

func assertLotIDsAndQuantities(t *testing.T, lots []portfolio.TaxLot, wantIDs, wantQuantities []string) {
	t.Helper()
	if len(lots) != len(wantIDs) || len(lots) != len(wantQuantities) {
		t.Fatalf("got %d lots, want %d IDs and quantities", len(lots), len(wantIDs))
	}
	for index, lot := range lots {
		if lot.ID() != wantIDs[index] {
			t.Errorf("lot[%d].ID() = %q, want %q", index, lot.ID(), wantIDs[index])
		}
		if got := lot.Quantity().String(); got != wantQuantities[index] {
			t.Errorf("lot[%d].Quantity() = %q, want %q", index, got, wantQuantities[index])
		}
	}
}

func assertValidationError(t *testing.T, err error, wantField string) {
	t.Helper()
	if wantField == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	var validationError *portfolio.ValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("error = %v, want *portfolio.ValidationError", err)
	}
	if validationError.Field != wantField {
		t.Fatalf("validation error field = %q, want %q", validationError.Field, wantField)
	}
}
