package portfolio

import (
	"sort"

	"github.com/shopspring/decimal"
)

const shareQuantityScale int32 = 8

// TradeSide identifies whether a rebalance recommendation buys or sells.
type TradeSide string

const (
	TradeSideBuy  TradeSide = "BUY"
	TradeSideSell TradeSide = "SELL"
)

// RebalanceTrade is a suggested order and its estimated cash and tax effects.
type RebalanceTrade struct {
	symbol       string
	side         TradeSide
	quantity     decimal.Decimal
	price        decimal.Decimal
	grossValue   decimal.Decimal
	commission   decimal.Decimal
	costBasis    decimal.Decimal
	realizedGain decimal.Decimal
	taxableGain  decimal.Decimal
	estimatedTax decimal.Decimal
	netCashFlow  decimal.Decimal
}

func (t RebalanceTrade) Symbol() string { return t.symbol }

func (t RebalanceTrade) Side() TradeSide { return t.side }

func (t RebalanceTrade) Quantity() decimal.Decimal { return t.quantity }

func (t RebalanceTrade) Price() decimal.Decimal { return t.price }

func (t RebalanceTrade) GrossValue() decimal.Decimal { return t.grossValue }

func (t RebalanceTrade) Commission() decimal.Decimal { return t.commission }

func (t RebalanceTrade) CostBasis() decimal.Decimal { return t.costBasis }

func (t RebalanceTrade) RealizedGain() decimal.Decimal { return t.realizedGain }

func (t RebalanceTrade) TaxableGain() decimal.Decimal { return t.taxableGain }

func (t RebalanceTrade) EstimatedTax() decimal.Decimal { return t.estimatedTax }

// NetCashFlow is positive for sale proceeds and negative for purchase outflow.
func (t RebalanceTrade) NetCashFlow() decimal.Decimal { return t.netCashFlow }

// RebalancePlan contains orders against current gross portfolio value. This
// model has no cash balance, so it reports net cash flow and required external
// cash separately; fees and taxes are not included in the target-value base.
type RebalancePlan struct {
	portfolioValue         decimal.Decimal
	buyValue               decimal.Decimal
	sellValue              decimal.Decimal
	commissionTotal        decimal.Decimal
	estimatedTaxTotal      decimal.Decimal
	netCashFlow            decimal.Decimal
	additionalCashRequired decimal.Decimal
	remainingCash          decimal.Decimal
	roundingResidual       decimal.Decimal
	trades                 []RebalanceTrade
}

func (p RebalancePlan) PortfolioValue() decimal.Decimal { return p.portfolioValue }

func (p RebalancePlan) BuyValue() decimal.Decimal { return p.buyValue }

func (p RebalancePlan) SellValue() decimal.Decimal { return p.sellValue }

func (p RebalancePlan) CommissionTotal() decimal.Decimal { return p.commissionTotal }

func (p RebalancePlan) EstimatedTaxTotal() decimal.Decimal { return p.estimatedTaxTotal }

func (p RebalancePlan) NetCashFlow() decimal.Decimal { return p.netCashFlow }

func (p RebalancePlan) AdditionalCashRequired() decimal.Decimal {
	return p.additionalCashRequired
}

func (p RebalancePlan) RemainingCash() decimal.Decimal { return p.remainingCash }

func (p RebalancePlan) RoundingResidual() decimal.Decimal { return p.roundingResidual }

func (p RebalancePlan) Trades() []RebalanceTrade {
	return append([]RebalanceTrade(nil), p.trades...)
}

// Rebalance calculates one order per affected symbol using the supplied latest
// quotes. Target values use the current gross market value before trading
// costs. Partial-order quantities are rounded down to eight decimal places;
// full liquidations preserve the exact recorded position quantity. The plan
// may retain a small allocation residual instead of overshooting a target.
// Positions without FIFO cost-basis lots cannot be sold for a tax estimate.
func (p Portfolio) Rebalance(quotes []Stock, commission, estimatedTaxRate decimal.Decimal) (RebalancePlan, error) {
	if _, err := NewPortfolio(p.positions, p.allocations); err != nil {
		return RebalancePlan{}, err
	}
	if err := validateTradingCostInputs(commission, estimatedTaxRate); err != nil {
		return RebalancePlan{}, err
	}

	quoteBySymbol := make(map[string]Stock, len(quotes))
	for _, quote := range quotes {
		if !validStock(quote) {
			return RebalancePlan{}, &ValidationError{Field: "quotes", Reason: "must contain only valid stocks"}
		}
		if _, exists := quoteBySymbol[quote.symbol]; exists {
			return RebalancePlan{}, &ValidationError{Field: "quotes", Reason: "must not contain duplicate symbols"}
		}
		quoteBySymbol[quote.symbol] = quote
	}

	positionBySymbol := make(map[string]Position, len(p.positions))
	targetWeightBySymbol := make(map[string]decimal.Decimal, len(p.allocations))
	symbolSet := make(map[string]struct{}, len(p.positions)+len(p.allocations))
	for _, position := range p.positions {
		positionBySymbol[position.stock.symbol] = position
		symbolSet[position.stock.symbol] = struct{}{}
	}
	for _, allocation := range p.allocations {
		targetWeightBySymbol[allocation.symbol] = allocation.targetWeight
		symbolSet[allocation.symbol] = struct{}{}
	}

	symbols := make([]string, 0, len(symbolSet))
	for symbol := range symbolSet {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	for _, symbol := range symbols {
		if _, exists := quoteBySymbol[symbol]; !exists {
			return RebalancePlan{}, &ValidationError{Field: "quotes", Reason: "missing current price for " + symbol}
		}
	}

	currentValueBySymbol := make(map[string]decimal.Decimal, len(positionBySymbol))
	portfolioValue := decimal.Zero
	for _, symbol := range symbols {
		position, exists := positionBySymbol[symbol]
		if !exists {
			continue
		}
		currentValue := position.quantity.Mul(quoteBySymbol[symbol].currentPrice)
		currentValueBySymbol[symbol] = currentValue
		portfolioValue = portfolioValue.Add(currentValue)
	}

	plan := RebalancePlan{portfolioValue: portfolioValue}
	postTradeValueBySymbol := make(map[string]decimal.Decimal, len(symbols))
	for _, symbol := range symbols {
		currentValue := currentValueBySymbol[symbol]
		targetValue := portfolioValue.Mul(targetWeightBySymbol[symbol])
		valueDifference := targetValue.Sub(currentValue)
		if valueDifference.IsZero() {
			postTradeValueBySymbol[symbol] = currentValue
			continue
		}

		quote := quoteBySymbol[symbol]
		if valueDifference.IsPositive() {
			quantity := quantityForTargetValue(valueDifference, quote.currentPrice)
			grossValue := quantity.Mul(quote.currentPrice)
			postTradeValueBySymbol[symbol] = currentValue.Add(grossValue)
			if !quantity.IsPositive() {
				continue
			}

			trade := RebalanceTrade{
				symbol:      symbol,
				side:        TradeSideBuy,
				quantity:    quantity,
				price:       quote.currentPrice,
				grossValue:  grossValue,
				commission:  commission,
				netCashFlow: grossValue.Add(commission).Neg(),
			}
			plan.trades = append(plan.trades, trade)
			plan.buyValue = plan.buyValue.Add(grossValue)
			plan.commissionTotal = plan.commissionTotal.Add(commission)
			plan.netCashFlow = plan.netCashFlow.Add(trade.netCashFlow)
			continue
		}

		position := positionBySymbol[symbol]
		saleQuantity := position.quantity
		if _, allocated := targetWeightBySymbol[symbol]; allocated {
			saleQuantity = quantityForTargetValue(valueDifference.Abs(), quote.currentPrice)
			if saleQuantity.GreaterThan(position.quantity) {
				saleQuantity = position.quantity
			}
		}
		if !saleQuantity.IsPositive() {
			postTradeValueBySymbol[symbol] = currentValue
			continue
		}

		sale, err := AllocateSaleFIFOAtPrice(position, saleQuantity, quote.currentPrice)
		if err != nil {
			return RebalancePlan{}, err
		}
		costs, err := EstimateSaleCosts(sale, commission, estimatedTaxRate)
		if err != nil {
			return RebalancePlan{}, err
		}
		trade := RebalanceTrade{
			symbol:       symbol,
			side:         TradeSideSell,
			quantity:     saleQuantity,
			price:        quote.currentPrice,
			grossValue:   sale.GrossProceeds(),
			commission:   costs.Commission(),
			costBasis:    sale.CostBasis(),
			realizedGain: sale.RealizedGain(),
			taxableGain:  costs.TaxableGain(),
			estimatedTax: costs.EstimatedTax(),
			netCashFlow:  costs.NetProceeds(),
		}
		plan.trades = append(plan.trades, trade)
		plan.sellValue = plan.sellValue.Add(trade.grossValue)
		plan.commissionTotal = plan.commissionTotal.Add(trade.commission)
		plan.estimatedTaxTotal = plan.estimatedTaxTotal.Add(trade.estimatedTax)
		plan.netCashFlow = plan.netCashFlow.Add(trade.netCashFlow)
		postTradeValueBySymbol[symbol] = currentValue.Sub(trade.grossValue)
	}

	for _, symbol := range symbols {
		targetValue := portfolioValue.Mul(targetWeightBySymbol[symbol])
		plan.roundingResidual = plan.roundingResidual.Add(postTradeValueBySymbol[symbol].Sub(targetValue).Abs())
	}
	if plan.netCashFlow.IsNegative() {
		plan.additionalCashRequired = plan.netCashFlow.Abs()
	} else {
		plan.remainingCash = plan.netCashFlow
	}

	return plan, nil
}

func quantityForTargetValue(value, price decimal.Decimal) decimal.Decimal {
	quantity := value.DivRound(price, shareQuantityScale)
	if quantity.Mul(price).GreaterThan(value) {
		quantity = quantity.Sub(decimal.New(1, -shareQuantityScale))
	}
	if quantity.IsNegative() {
		return decimal.Zero
	}
	return quantity
}
