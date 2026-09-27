package portfolio

import (
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ValidationError reports invalid portfolio domain data.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Reason
}

// Stock represents a publicly traded instrument and its latest known price.
type Stock struct {
	symbol         string
	currentPrice   decimal.Decimal
	priceUpdatedAt time.Time
}

// NewStock creates a stock with a positive price and a known quote timestamp.
func NewStock(symbol string, currentPrice decimal.Decimal, priceUpdatedAt time.Time) (Stock, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return Stock{}, &ValidationError{Field: "symbol", Reason: "must not be empty"}
	}
	if !currentPrice.IsPositive() {
		return Stock{}, &ValidationError{Field: "currentPrice", Reason: "must be greater than zero"}
	}
	if priceUpdatedAt.IsZero() {
		return Stock{}, &ValidationError{Field: "priceUpdatedAt", Reason: "must be provided"}
	}

	return Stock{symbol: symbol, currentPrice: currentPrice, priceUpdatedAt: priceUpdatedAt}, nil
}

func (s Stock) Symbol() string { return s.symbol }

func (s Stock) CurrentPrice() decimal.Decimal { return s.currentPrice }

func (s Stock) PriceUpdatedAt() time.Time { return s.priceUpdatedAt }

// WithCurrentPrice returns a validated copy containing the latest available
// price and its observation time; the original Stock remains unchanged.
func (s Stock) WithCurrentPrice(price decimal.Decimal, updatedAt time.Time) (Stock, error) {
	return NewStock(s.symbol, price, updatedAt)
}

// Position describes how many shares of a stock the portfolio currently owns.
type Position struct {
	stock    Stock
	quantity decimal.Decimal
	lots     []TaxLot
}

// NewPosition creates a position with a positive share quantity.
func NewPosition(stock Stock, quantity decimal.Decimal) (Position, error) {
	if !validStock(stock) {
		return Position{}, &ValidationError{Field: "stock", Reason: "must be a valid stock"}
	}
	if !quantity.IsPositive() {
		return Position{}, &ValidationError{Field: "quantity", Reason: "must be greater than zero"}
	}

	return Position{stock: stock, quantity: quantity}, nil
}

// TaxLot records the quantity and unit acquisition price from one purchase.
type TaxLot struct {
	id         string
	quantity   decimal.Decimal
	unitCost   decimal.Decimal
	acquiredAt time.Time
}

// NewTaxLot creates a valid acquisition lot. The ID provides a stable FIFO
// tie-breaker when multiple lots have the same acquisition timestamp.
func NewTaxLot(id string, quantity, unitCost decimal.Decimal, acquiredAt time.Time) (TaxLot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return TaxLot{}, &ValidationError{Field: "lotID", Reason: "must not be empty"}
	}
	if !quantity.IsPositive() {
		return TaxLot{}, &ValidationError{Field: "quantity", Reason: "must be greater than zero"}
	}
	if !unitCost.IsPositive() {
		return TaxLot{}, &ValidationError{Field: "unitCost", Reason: "must be greater than zero"}
	}
	if acquiredAt.IsZero() {
		return TaxLot{}, &ValidationError{Field: "acquiredAt", Reason: "must be provided"}
	}

	return TaxLot{id: id, quantity: quantity, unitCost: unitCost, acquiredAt: acquiredAt}, nil
}

func (l TaxLot) ID() string { return l.id }

func (l TaxLot) Quantity() decimal.Decimal { return l.quantity }

func (l TaxLot) UnitCost() decimal.Decimal { return l.unitCost }

func (l TaxLot) AcquiredAt() time.Time { return l.acquiredAt }

// NewPositionWithLots creates a position whose quantity is derived from its
// acquisition lots. Lot IDs must be unique within the position.
func NewPositionWithLots(stock Stock, lots []TaxLot) (Position, error) {
	if !validStock(stock) {
		return Position{}, &ValidationError{Field: "stock", Reason: "must be a valid stock"}
	}
	if len(lots) == 0 {
		return Position{}, &ValidationError{Field: "lots", Reason: "must not be empty"}
	}

	seenIDs := make(map[string]struct{}, len(lots))
	quantity := decimal.Zero
	for _, lot := range lots {
		if !validTaxLot(lot) {
			return Position{}, &ValidationError{Field: "lots", Reason: "must contain only valid lots"}
		}
		if _, exists := seenIDs[lot.id]; exists {
			return Position{}, &ValidationError{Field: "lots", Reason: "lot IDs must be unique"}
		}
		seenIDs[lot.id] = struct{}{}
		quantity = quantity.Add(lot.quantity)
	}

	return Position{
		stock:    stock,
		quantity: quantity,
		lots:     append([]TaxLot(nil), lots...),
	}, nil
}

func (p Position) Stock() Stock { return p.stock }

func (p Position) Quantity() decimal.Decimal { return p.quantity }

// Lots returns a copy of the position's acquisition lots.
func (p Position) Lots() []TaxLot { return append([]TaxLot(nil), p.lots...) }

// SaleAllocation is the exact FIFO breakdown for a proposed sale.
type SaleAllocation struct {
	quantity      decimal.Decimal
	costBasis     decimal.Decimal
	grossProceeds decimal.Decimal
	realizedGain  decimal.Decimal
	soldLots      []TaxLot
	remainingLots []TaxLot
}

func (s SaleAllocation) Quantity() decimal.Decimal { return s.quantity }

func (s SaleAllocation) CostBasis() decimal.Decimal { return s.costBasis }

func (s SaleAllocation) GrossProceeds() decimal.Decimal { return s.grossProceeds }

func (s SaleAllocation) RealizedGain() decimal.Decimal { return s.realizedGain }

func (s SaleAllocation) SoldLots() []TaxLot { return append([]TaxLot(nil), s.soldLots...) }

func (s SaleAllocation) RemainingLots() []TaxLot {
	return append([]TaxLot(nil), s.remainingLots...)
}

// AllocateSaleFIFO calculates a sale's cost basis using oldest acquisition
// lots first. It returns new lot slices and does not mutate the position.
// Equal acquisition timestamps are ordered by lot ID for deterministic output.
func AllocateSaleFIFO(position Position, quantity decimal.Decimal) (SaleAllocation, error) {
	return AllocateSaleFIFOAtPrice(position, quantity, position.stock.currentPrice)
}

// AllocateSaleFIFOAtPrice allocates a sale against FIFO lots using the supplied
// latest quote rather than the quote stored when the position was created.
func AllocateSaleFIFOAtPrice(position Position, quantity, salePrice decimal.Decimal) (SaleAllocation, error) {
	if !position.valid() {
		return SaleAllocation{}, &ValidationError{Field: "position", Reason: "must be valid"}
	}
	if len(position.lots) == 0 {
		return SaleAllocation{}, &ValidationError{Field: "lots", Reason: "cost basis is required for a sale estimate"}
	}
	if !quantity.IsPositive() || quantity.GreaterThan(position.quantity) {
		return SaleAllocation{}, &ValidationError{Field: "quantity", Reason: "must be greater than zero and no greater than the position quantity"}
	}
	if !salePrice.IsPositive() {
		return SaleAllocation{}, &ValidationError{Field: "salePrice", Reason: "must be greater than zero"}
	}

	lots := position.Lots()
	sort.Slice(lots, func(i, j int) bool {
		if lots[i].acquiredAt.Equal(lots[j].acquiredAt) {
			return lots[i].id < lots[j].id
		}
		return lots[i].acquiredAt.Before(lots[j].acquiredAt)
	})

	allocation := SaleAllocation{quantity: quantity}
	remainingToSell := quantity
	for _, lot := range lots {
		if remainingToSell.IsZero() {
			allocation.remainingLots = append(allocation.remainingLots, lot)
			continue
		}

		soldQuantity := lot.quantity
		if soldQuantity.GreaterThan(remainingToSell) {
			soldQuantity = remainingToSell
		}
		soldLot := lot
		soldLot.quantity = soldQuantity
		allocation.soldLots = append(allocation.soldLots, soldLot)
		allocation.costBasis = allocation.costBasis.Add(soldQuantity.Mul(lot.unitCost))

		unsoldQuantity := lot.quantity.Sub(soldQuantity)
		if unsoldQuantity.IsPositive() {
			remainingLot := lot
			remainingLot.quantity = unsoldQuantity
			allocation.remainingLots = append(allocation.remainingLots, remainingLot)
		}
		remainingToSell = remainingToSell.Sub(soldQuantity)
	}

	allocation.grossProceeds = quantity.Mul(salePrice)
	allocation.realizedGain = allocation.grossProceeds.Sub(allocation.costBasis)
	return allocation, nil
}

// Allocation describes a target portfolio weight as a fraction from 0 to 1.
// For example, 40% is represented exactly as decimal.NewFromString("0.4").
type Allocation struct {
	symbol       string
	targetWeight decimal.Decimal
}

// NewAllocation creates an allocation with a target weight greater than 0 and
// no greater than 1. NewPortfolio verifies that all weights total exactly 1.
func NewAllocation(symbol string, targetWeight decimal.Decimal) (Allocation, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return Allocation{}, &ValidationError{Field: "symbol", Reason: "must not be empty"}
	}
	if !targetWeight.IsPositive() || targetWeight.GreaterThan(decimal.NewFromInt(1)) {
		return Allocation{}, &ValidationError{Field: "targetWeight", Reason: "must be greater than zero and no greater than one"}
	}

	return Allocation{symbol: symbol, targetWeight: targetWeight}, nil
}

func (a Allocation) Symbol() string { return a.symbol }

func (a Allocation) TargetWeight() decimal.Decimal { return a.targetWeight }

// Portfolio stores current positions separately from the target allocation.
// The separate collections let rebalance logic compare holdings with intent.
type Portfolio struct {
	positions   []Position
	allocations []Allocation
}

// NewPortfolio copies and validates positions and allocations. Target weights
// must sum exactly to 1, and each collection may contain a symbol only once.
func NewPortfolio(positions []Position, allocations []Allocation) (Portfolio, error) {
	if len(allocations) == 0 {
		return Portfolio{}, &ValidationError{Field: "allocations", Reason: "must not be empty"}
	}

	positionSymbols := make(map[string]struct{}, len(positions))
	for _, position := range positions {
		symbol := position.stock.symbol
		if !position.valid() {
			return Portfolio{}, &ValidationError{Field: "positions", Reason: "must contain only valid positions"}
		}
		if _, exists := positionSymbols[symbol]; exists {
			return Portfolio{}, &ValidationError{Field: "positions", Reason: "must not contain duplicate symbols"}
		}
		positionSymbols[symbol] = struct{}{}
	}

	allocationSymbols := make(map[string]struct{}, len(allocations))
	totalWeight := decimal.Zero
	for _, allocation := range allocations {
		if allocation.symbol == "" || !allocation.targetWeight.IsPositive() || allocation.targetWeight.GreaterThan(decimal.NewFromInt(1)) {
			return Portfolio{}, &ValidationError{Field: "allocations", Reason: "must contain only valid allocations"}
		}
		if _, exists := allocationSymbols[allocation.symbol]; exists {
			return Portfolio{}, &ValidationError{Field: "allocations", Reason: "must not contain duplicate symbols"}
		}
		allocationSymbols[allocation.symbol] = struct{}{}
		totalWeight = totalWeight.Add(allocation.targetWeight)
	}
	if !totalWeight.Equal(decimal.NewFromInt(1)) {
		return Portfolio{}, &ValidationError{Field: "allocations", Reason: "target weights must sum exactly to one"}
	}

	return Portfolio{
		positions:   append([]Position(nil), positions...),
		allocations: append([]Allocation(nil), allocations...),
	}, nil
}

func (p Portfolio) Positions() []Position {
	return append([]Position(nil), p.positions...)
}

func (p Portfolio) Allocations() []Allocation {
	return append([]Allocation(nil), p.allocations...)
}

func (p Position) valid() bool {
	if !validStock(p.stock) || !p.quantity.IsPositive() {
		return false
	}
	if len(p.lots) == 0 {
		return true
	}

	seenIDs := make(map[string]struct{}, len(p.lots))
	lotQuantity := decimal.Zero
	for _, lot := range p.lots {
		if !validTaxLot(lot) {
			return false
		}
		if _, exists := seenIDs[lot.id]; exists {
			return false
		}
		seenIDs[lot.id] = struct{}{}
		lotQuantity = lotQuantity.Add(lot.quantity)
	}
	return lotQuantity.Equal(p.quantity)
}

func validStock(stock Stock) bool {
	return stock.symbol != "" && stock.currentPrice.IsPositive() && !stock.priceUpdatedAt.IsZero()
}

func validTaxLot(lot TaxLot) bool {
	return lot.id != "" && lot.quantity.IsPositive() && lot.unitCost.IsPositive() && !lot.acquiredAt.IsZero()
}
