package momentum_test

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"portfolio/marketdata"
	"portfolio/momentum"
)

func TestCalculate_UsesFormulaAndCompletedMonths(t *testing.T) {
	asOf := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	bars := risingBars(15, time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), "100", "1")
	partialMonth := makeBar(t, time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), "1000", "1001", "999", "1000")
	barsWithPartial := append(append([]marketdata.MonthlyBar(nil), bars...), partialMonth)

	tests := []struct {
		name string
		bars []marketdata.MonthlyBar
	}{
		{name: "completed monthly history", bars: bars},
		{name: "ignores current incomplete month", bars: barsWithPartial},
	}
	var baseline momentum.Score
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			score, err := momentum.Calculate(" test ", test.bars, asOf)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}
			if score.Symbol != "TEST" {
				t.Fatalf("Symbol = %q, want TEST", score.Symbol)
			}
			if got := score.ROC8.String(); got != "7.5471698113207547" {
				t.Fatalf("ROC8 = %q, want 7.5471698113207547", got)
			}
			if got := score.ROC10.String(); got != "9.6153846153846154" {
				t.Fatalf("ROC10 = %q, want 9.6153846153846154", got)
			}
			if got := score.ATR14.String(); got != "2" {
				t.Fatalf("ATR14 = %q, want 2", got)
			}
			if got := score.Average14.String(); got != "107.5" {
				t.Fatalf("Average14 = %q, want 107.5", got)
			}
			if !score.AsOf.Equal(bars[len(bars)-1].Time) {
				t.Fatalf("AsOf = %s, want last completed bar %s", score.AsOf, bars[len(bars)-1].Time)
			}
			if test.name == "completed monthly history" {
				baseline = score
			} else if !score.Value.Equal(baseline.Value) {
				t.Fatalf("current partial month changed score to %s; want %s", score.Value, baseline.Value)
			}
		})
	}
}

func TestCalculate_RejectsInvalidHistory(t *testing.T) {
	asOf := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	validBars := risingBars(15, time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), "100", "1")
	shortHistory := append([]marketdata.MonthlyBar(nil), validBars[:14]...)
	duplicateMonth := append([]marketdata.MonthlyBar(nil), validBars...)
	duplicateMonth[14].Time = duplicateMonth[13].Time.Add(2 * time.Hour)
	invalidOHLC := append([]marketdata.MonthlyBar(nil), validBars...)
	invalidOHLC[10].Low = decimal.NewFromInt(500)
	missingTime := append([]marketdata.MonthlyBar(nil), validBars...)
	missingTime[4].Time = time.Time{}

	tests := []struct {
		name   string
		symbol string
		bars   []marketdata.MonthlyBar
		asOf   time.Time
	}{
		{name: "empty symbol", bars: validBars, asOf: asOf},
		{name: "missing as-of", symbol: "TEST", bars: validBars},
		{name: "short history", symbol: "TEST", bars: shortHistory, asOf: asOf},
		{name: "duplicate monthly bars", symbol: "TEST", bars: duplicateMonth, asOf: asOf},
		{name: "invalid OHLC", symbol: "TEST", bars: invalidOHLC, asOf: asOf},
		{name: "missing timestamp", symbol: "TEST", bars: missingTime, asOf: asOf},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := momentum.Calculate(test.symbol, test.bars, test.asOf); err == nil {
				t.Fatal("Calculate() error = nil, want error")
			}
		})
	}
}

func TestRank_SortsDescendingAndBreaksTiesBySymbol(t *testing.T) {
	asOf := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	flatHistory := monthlyHistory("BBB", risingBars(15, time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), "100", "1"))
	tiedHistory := monthlyHistory("AAA", flatHistory.Bars)
	fasterHistory := monthlyHistory("FAST", risingBars(15, time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), "100", "2"))

	ranked, err := momentum.Rank([]marketdata.MonthlyHistory{flatHistory, fasterHistory, tiedHistory}, asOf)
	if err != nil {
		t.Fatalf("Rank() error = %v", err)
	}
	if len(ranked) != 3 || ranked[0].Symbol != "FAST" || ranked[1].Symbol != "AAA" || ranked[2].Symbol != "BBB" {
		t.Fatalf("ranked symbols = %s", joinSymbols(ranked))
	}
	if !ranked[1].Value.Equal(ranked[2].Value) {
		t.Fatalf("tie scores differ: AAA=%s BBB=%s", ranked[1].Value, ranked[2].Value)
	}

	top, err := momentum.Top(ranked, 2)
	if err != nil {
		t.Fatalf("Top() error = %v", err)
	}
	if len(top) != 2 || top[0].Symbol != "FAST" || top[1].Symbol != "AAA" {
		t.Fatalf("Top(2) symbols = %s, want FAST,AAA", joinSymbols(top))
	}
}

func TestRank_FailsRatherThanSilentlyDroppingCandidates(t *testing.T) {
	asOf := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	shortHistory := monthlyHistory("NEW", risingBars(10, time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), "100", "1"))
	validHistory := monthlyHistory("VALID", risingBars(15, time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), "100", "1"))
	if _, err := momentum.Rank([]marketdata.MonthlyHistory{shortHistory}, asOf); err == nil {
		t.Fatal("Rank() error = nil for insufficient history, want error")
	}
	duplicateHistories := []marketdata.MonthlyHistory{validHistory, validHistory}
	if _, err := momentum.Rank(duplicateHistories, asOf); err == nil || !strings.Contains(err.Error(), "duplicate symbol") {
		t.Fatalf("Rank() error = %v, want duplicate-symbol error", err)
	}
	wrongCurrency := validHistory
	wrongCurrency.Currency = "EUR"
	if _, err := momentum.Rank([]marketdata.MonthlyHistory{wrongCurrency}, asOf); err == nil || !strings.Contains(err.Error(), "USD") {
		t.Fatalf("Rank() error = %v, want currency error", err)
	}
}

func TestTop_RequiresEnoughAssetsAndAllowsNegativeScores(t *testing.T) {
	tests := []struct {
		name   string
		scores []momentum.Score
		count  int
		want   []string
		err    bool
	}{
		{name: "takes requested number even when all scores are negative", scores: []momentum.Score{{Symbol: "A", Value: decimal.NewFromInt(-1)}, {Symbol: "B", Value: decimal.NewFromInt(-2)}}, count: 2, want: []string{"A", "B"}},
		{name: "rejects non-positive count", scores: []momentum.Score{{Symbol: "A"}}, count: 0, err: true},
		{name: "rejects too few scored assets", scores: []momentum.Score{{Symbol: "A"}}, count: 2, err: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			top, err := momentum.Top(test.scores, test.count)
			if test.err {
				if err == nil {
					t.Fatal("Top() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Top() error = %v", err)
			}
			if got := joinSymbols(top); got != strings.Join(test.want, ",") {
				t.Fatalf("Top() = %s, want %s", got, strings.Join(test.want, ","))
			}
		})
	}
}

func TestEqualWeightAllocations(t *testing.T) {
	tests := []struct {
		name   string
		scores []momentum.Score
		want   []string
		err    bool
	}{
		{
			name:   "three targets sum exactly to one",
			scores: []momentum.Score{{Symbol: "META"}, {Symbol: "AAPL"}, {Symbol: "NVDA"}},
			want:   []string{"0.333333333333333333", "0.333333333333333333", "0.333333333333333334"},
		},
		{name: "rejects empty selection", err: true},
		{name: "rejects duplicate symbols", scores: []momentum.Score{{Symbol: "META"}, {Symbol: " meta "}}, err: true},
		{name: "rejects empty symbol", scores: []momentum.Score{{Symbol: " "}}, err: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocations, err := momentum.EqualWeightAllocations(test.scores)
			if test.err {
				if err == nil {
					t.Fatal("EqualWeightAllocations() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("EqualWeightAllocations() error = %v", err)
			}
			if len(allocations) != len(test.want) {
				t.Fatalf("got %d allocations, want %d", len(allocations), len(test.want))
			}
			total := decimal.Zero
			for index, allocation := range allocations {
				if got := allocation.TargetWeight().String(); got != test.want[index] {
					t.Errorf("weight[%d] = %q, want %q", index, got, test.want[index])
				}
				total = total.Add(allocation.TargetWeight())
			}
			if !total.Equal(decimal.NewFromInt(1)) {
				t.Errorf("weights total = %s, want 1", total)
			}
		})
	}
}

func risingBars(count int, start time.Time, initialClose, increment string) []marketdata.MonthlyBar {
	initial := decimal.RequireFromString(initialClose)
	step := decimal.RequireFromString(increment)
	bars := make([]marketdata.MonthlyBar, 0, count)
	for index := 0; index < count; index++ {
		closePrice := initial.Add(step.Mul(decimal.NewFromInt(int64(index))))
		bars = append(bars, marketdata.MonthlyBar{
			Time:  start.AddDate(0, index, 0),
			Open:  closePrice,
			High:  closePrice.Add(decimal.NewFromInt(1)),
			Low:   closePrice.Sub(decimal.NewFromInt(1)),
			Close: closePrice,
		})
	}
	return bars
}

func makeBar(t *testing.T, timestamp time.Time, open, high, low, closePrice string) marketdata.MonthlyBar {
	t.Helper()
	return marketdata.MonthlyBar{
		Time:  timestamp,
		Open:  decimal.RequireFromString(open),
		High:  decimal.RequireFromString(high),
		Low:   decimal.RequireFromString(low),
		Close: decimal.RequireFromString(closePrice),
	}
}

func monthlyHistory(symbol string, bars []marketdata.MonthlyBar) marketdata.MonthlyHistory {
	return marketdata.MonthlyHistory{Symbol: symbol, Currency: "USD", Bars: bars}
}

func joinSymbols(scores []momentum.Score) string {
	symbols := make([]string, len(scores))
	for index, score := range scores {
		symbols[index] = score.Symbol
	}
	return strings.Join(symbols, ",")
}
