package portfolio

import "github.com/shopspring/decimal"

const usdMinorUnitScale int32 = 2

// SaleCostEstimate reports estimated costs and proceeds for one sell order.
type SaleCostEstimate struct {
	commission      decimal.Decimal
	taxableGain     decimal.Decimal
	estimatedTax    decimal.Decimal
	netProceeds     decimal.Decimal
	netRealizedGain decimal.Decimal
}

func (e SaleCostEstimate) Commission() decimal.Decimal { return e.commission }

func (e SaleCostEstimate) TaxableGain() decimal.Decimal { return e.taxableGain }

func (e SaleCostEstimate) EstimatedTax() decimal.Decimal { return e.estimatedTax }

func (e SaleCostEstimate) NetProceeds() decimal.Decimal { return e.netProceeds }

func (e SaleCostEstimate) NetRealizedGain() decimal.Decimal { return e.netRealizedGain }

// EstimateSaleCosts applies one fixed commission to one sale order. The tax
// estimate is max(realized gain minus commission, 0) multiplied by the
// configured rate. It is rounded to USD cents using decimal's half-away-from-
// zero rule only after all preceding calculations remain exact.
func EstimateSaleCosts(sale SaleAllocation, commission, estimatedTaxRate decimal.Decimal) (SaleCostEstimate, error) {
	if !sale.quantity.IsPositive() || !sale.grossProceeds.IsPositive() || sale.costBasis.IsNegative() || !sale.grossProceeds.Sub(sale.costBasis).Equal(sale.realizedGain) {
		return SaleCostEstimate{}, &ValidationError{Field: "sale", Reason: "must be a valid sale allocation"}
	}
	if err := validateTradingCostInputs(commission, estimatedTaxRate); err != nil {
		return SaleCostEstimate{}, err
	}

	taxableGain := sale.realizedGain.Sub(commission)
	if taxableGain.IsNegative() {
		taxableGain = decimal.Zero
	}
	estimatedTax := taxableGain.Mul(estimatedTaxRate).Round(usdMinorUnitScale)

	return SaleCostEstimate{
		commission:      commission,
		taxableGain:     taxableGain,
		estimatedTax:    estimatedTax,
		netProceeds:     sale.grossProceeds.Sub(commission).Sub(estimatedTax),
		netRealizedGain: sale.realizedGain.Sub(commission).Sub(estimatedTax),
	}, nil
}

// validateTradingCostInputs validates the shared costs passed to a plan or sale estimate.
func validateTradingCostInputs(commission, estimatedTaxRate decimal.Decimal) error {
	if commission.IsNegative() || !commission.Equal(commission.Round(usdMinorUnitScale)) {
		return &ValidationError{Field: "commission", Reason: "must be non-negative and use no more than two decimal places"}
	}
	if estimatedTaxRate.IsNegative() || estimatedTaxRate.GreaterThan(decimal.NewFromInt(1)) {
		return &ValidationError{Field: "estimatedTaxRate", Reason: "must be between zero and one"}
	}
	return nil
}
