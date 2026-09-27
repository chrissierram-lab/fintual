package portfolio_test

import (
	"strconv"
	"testing"

	"portfolio"
)

func TestEstimateSaleCosts(t *testing.T) {
	profitableSale := mustSaleAllocation(t, "15", "1.5", "1", "10", "2", "12")
	smallProfitSale := mustSaleAllocation(t, "15", "0.5", "1", "10")
	lossSale := mustSaleAllocation(t, "15", "1", "1", "20")
	halfCentTaxSale := mustSaleAllocation(t, "1", "1", "1", "0.995")
	saleSmallerThanFee := mustSaleAllocation(t, "0.5", "1", "1", "0.1")

	tests := []struct {
		name                string
		sale                portfolio.SaleAllocation
		commission          string
		taxRate             string
		wantTaxableGain     string
		wantEstimatedTax    string
		wantNetProceeds     string
		wantNetRealizedGain string
		wantErrorField      string
	}{
		{
			name:                "applies one fee then estimates tax on net positive gain",
			sale:                profitableSale,
			commission:          "0.99",
			taxRate:             "0.15",
			wantTaxableGain:     "5.51",
			wantEstimatedTax:    "0.83",
			wantNetProceeds:     "20.68",
			wantNetRealizedGain: "4.68",
		},
		{
			name:                "supports a zero estimated tax rate",
			sale:                profitableSale,
			commission:          "0.99",
			taxRate:             "0",
			wantTaxableGain:     "5.51",
			wantEstimatedTax:    "0",
			wantNetProceeds:     "21.51",
			wantNetRealizedGain: "5.51",
		},
		{
			name:                "does not estimate tax when commission consumes the gain",
			sale:                smallProfitSale,
			commission:          "3.00",
			taxRate:             "0.15",
			wantTaxableGain:     "0",
			wantEstimatedTax:    "0",
			wantNetProceeds:     "4.5",
			wantNetRealizedGain: "-0.5",
		},
		{
			name:                "does not estimate tax on a realized loss",
			sale:                lossSale,
			commission:          "0.99",
			taxRate:             "0.15",
			wantTaxableGain:     "0",
			wantEstimatedTax:    "0",
			wantNetProceeds:     "14.01",
			wantNetRealizedGain: "-5.99",
		},
		{
			name:                "rounds a half cent away from zero at the final tax amount",
			sale:                halfCentTaxSale,
			commission:          "0",
			taxRate:             "1",
			wantTaxableGain:     "0.005",
			wantEstimatedTax:    "0.01",
			wantNetProceeds:     "0.99",
			wantNetRealizedGain: "-0.005",
		},
		{
			name:                "reports negative net proceeds when fee exceeds sale value",
			sale:                saleSmallerThanFee,
			commission:          "0.99",
			taxRate:             "0.15",
			wantTaxableGain:     "0",
			wantEstimatedTax:    "0",
			wantNetProceeds:     "-0.49",
			wantNetRealizedGain: "-0.59",
		},
		{name: "rejects negative commission", sale: profitableSale, commission: "-0.01", taxRate: "0.15", wantErrorField: "commission"},
		{name: "rejects commission with sub-cent precision", sale: profitableSale, commission: "0.001", taxRate: "0.15", wantErrorField: "commission"},
		{name: "rejects negative tax rate", sale: profitableSale, commission: "0.99", taxRate: "-0.01", wantErrorField: "estimatedTaxRate"},
		{name: "rejects tax rate above one", sale: profitableSale, commission: "0.99", taxRate: "1.01", wantErrorField: "estimatedTaxRate"},
		{name: "rejects an empty sale allocation", commission: "0", taxRate: "0", wantErrorField: "sale"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := portfolio.EstimateSaleCosts(test.sale, mustDecimal(t, test.commission), mustDecimal(t, test.taxRate))
			assertValidationError(t, err, test.wantErrorField)
			if err != nil {
				return
			}
			if got := result.TaxableGain().String(); got != test.wantTaxableGain {
				t.Errorf("TaxableGain() = %q, want %q", got, test.wantTaxableGain)
			}
			if got := result.EstimatedTax().String(); got != test.wantEstimatedTax {
				t.Errorf("EstimatedTax() = %q, want %q", got, test.wantEstimatedTax)
			}
			if got := result.NetProceeds().String(); got != test.wantNetProceeds {
				t.Errorf("NetProceeds() = %q, want %q", got, test.wantNetProceeds)
			}
			if got := result.NetRealizedGain().String(); got != test.wantNetRealizedGain {
				t.Errorf("NetRealizedGain() = %q, want %q", got, test.wantNetRealizedGain)
			}
			if !result.Commission().Equal(mustDecimal(t, test.commission)) {
				t.Errorf("Commission() = %s, want %s", result.Commission(), test.commission)
			}
		})
	}
}

func mustSaleAllocation(t *testing.T, currentPrice, quantity string, lotPairs ...string) portfolio.SaleAllocation {
	t.Helper()
	if len(lotPairs)%2 != 0 {
		t.Fatal("lotPairs must contain quantity and unit cost pairs")
	}
	stock := mustStock(t, "META", currentPrice)
	lots := make([]portfolio.TaxLot, 0, len(lotPairs)/2)
	for index := 0; index < len(lotPairs); index += 2 {
		lots = append(lots, mustTaxLot(t, "lot-"+strconv.Itoa(index/2), lotPairs[index], lotPairs[index+1], index/2+1))
	}
	position := mustPositionWithLots(t, stock, lots...)
	allocation, err := portfolio.AllocateSaleFIFO(position, mustDecimal(t, quantity))
	if err != nil {
		t.Fatalf("AllocateSaleFIFO(): %v", err)
	}
	return allocation
}
