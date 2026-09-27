package momentum

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"portfolio"
	"portfolio/marketdata"
)

const (
	rocShortPeriod       = 8
	rocLongPeriod        = 10
	atrPeriod            = 14
	averagePeriod        = 14
	decimalScale   int32 = 18
)

var (
	weightROCShort = decimal.RequireFromString("0.4")
	weightROCLong  = decimal.RequireFromString("0.2")
	weightATR      = decimal.RequireFromString("0.4")
	percentage     = decimal.NewFromInt(100)
)

// Score contains the momentum score and its auditable components.
type Score struct {
	Symbol        string          `json:"symbol"`
	Value         decimal.Decimal `json:"value"`
	ROC8          decimal.Decimal `json:"roc8"`
	ROC10         decimal.Decimal `json:"roc10"`
	ATR14         decimal.Decimal `json:"atr14"`
	Average14     decimal.Decimal `json:"average14"`
	RelativeATR14 decimal.Decimal `json:"relativeAtr14"`
	AsOf          time.Time       `json:"asOf"`
}

// Calculate computes the ProRealTime-style score from completed monthly bars:
// (ROC8*0.4 + ROC10*0.2) / ((WilderATR14/SMA14)*0.4).
// ROC values are percentage points; ATR uses Wilder smoothing, average14 is a
// simple mean, and the current calendar month is excluded.
func Calculate(symbol string, inputBars []marketdata.MonthlyBar, asOf time.Time) (Score, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return Score{}, fmt.Errorf("symbol must not be empty")
	}
	if asOf.IsZero() {
		return Score{}, fmt.Errorf("as-of time must be provided")
	}
	if len(inputBars) == 0 {
		return Score{}, fmt.Errorf("%s has no monthly bars", symbol)
	}

	monthStart := time.Date(asOf.UTC().Year(), asOf.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	bars := make([]marketdata.MonthlyBar, 0, len(inputBars))
	for _, bar := range inputBars {
		if bar.Time.IsZero() {
			return Score{}, fmt.Errorf("%s contains a bar without a timestamp", symbol)
		}
		if !bar.Time.UTC().Before(monthStart) {
			continue
		}
		if !validBar(bar) {
			return Score{}, fmt.Errorf("%s has invalid OHLC values for %s", symbol, bar.Time.UTC().Format("2006-01"))
		}
		bar.Time = bar.Time.UTC()
		bars = append(bars, bar)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Time.Before(bars[j].Time) })
	for index := 1; index < len(bars); index++ {
		previous := bars[index-1].Time
		current := bars[index].Time
		if previous.Year() == current.Year() && previous.Month() == current.Month() {
			return Score{}, fmt.Errorf("%s has duplicate monthly bars for %s", symbol, current.Format("2006-01"))
		}
	}
	if len(bars) < atrPeriod+1 {
		return Score{}, fmt.Errorf("%s requires at least %d completed monthly bars; got %d", symbol, atrPeriod+1, len(bars))
	}

	lastIndex := len(bars) - 1
	roc8 := bars[lastIndex].Close.DivRound(bars[lastIndex-rocShortPeriod].Close, decimalScale).Sub(decimal.NewFromInt(1)).Mul(percentage)
	roc10 := bars[lastIndex].Close.DivRound(bars[lastIndex-rocLongPeriod].Close, decimalScale).Sub(decimal.NewFromInt(1)).Mul(percentage)
	average14 := decimal.Zero
	for index := len(bars) - averagePeriod; index < len(bars); index++ {
		average14 = average14.Add(bars[index].Close)
	}
	average14 = average14.DivRound(decimal.NewFromInt(averagePeriod), decimalScale)

	atr14, err := wilderATR(bars, atrPeriod)
	if err != nil {
		return Score{}, fmt.Errorf("calculate %s ATR: %w", symbol, err)
	}
	relativeATR := atr14.DivRound(average14, decimalScale)
	weightedMomentum := roc8.Mul(weightROCShort).Add(roc10.Mul(weightROCLong))
	denominator := relativeATR.Mul(weightATR)
	if !denominator.IsPositive() {
		return Score{}, fmt.Errorf("%s has zero relative volatility", symbol)
	}
	value := weightedMomentum.DivRound(denominator, decimalScale)

	return Score{
		Symbol:        symbol,
		Value:         value,
		ROC8:          roc8,
		ROC10:         roc10,
		ATR14:         atr14,
		Average14:     average14,
		RelativeATR14: relativeATR,
		AsOf:          bars[lastIndex].Time,
	}, nil
}

// Rank calculates every supplied history and sorts by score descending, with
// alphabetical symbol order as a deterministic tie-breaker. A bad/short history
// fails the entire ranking rather than silently changing the candidate universe.
func Rank(histories []marketdata.MonthlyHistory, asOf time.Time) ([]Score, error) {
	if len(histories) == 0 {
		return nil, fmt.Errorf("candidate universe has no histories")
	}

	scores := make([]Score, 0, len(histories))
	seenSymbols := make(map[string]struct{}, len(histories))
	for _, history := range histories {
		symbol := strings.ToUpper(strings.TrimSpace(history.Symbol))
		if _, exists := seenSymbols[symbol]; exists {
			return nil, fmt.Errorf("candidate universe contains duplicate symbol %s", symbol)
		}
		seenSymbols[symbol] = struct{}{}
		if !strings.EqualFold(history.Currency, "USD") {
			return nil, fmt.Errorf("%s history currency must be USD", symbol)
		}
		score, err := Calculate(symbol, history.Bars, asOf)
		if err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Value.Equal(scores[j].Value) {
			return scores[i].Symbol < scores[j].Symbol
		}
		return scores[i].Value.GreaterThan(scores[j].Value)
	})
	return scores, nil
}

// Top returns the highest-ranked count of assets. It does not filter negative
// scores: the caller asked to maintain exactly the top three assets.
func Top(scores []Score, count int) ([]Score, error) {
	if count <= 0 {
		return nil, fmt.Errorf("top count must be positive")
	}
	if len(scores) < count {
		return nil, fmt.Errorf("need at least %d ranked assets; got %d", count, len(scores))
	}
	result := append([]Score(nil), scores[:count]...)
	return result, nil
}

// EqualWeightAllocations creates exact-sum target weights for the selected
// scores. A repeating fraction such as one third is represented to 18 decimal
// places; the final allocation receives the tiny remainder so weights total 1.
func EqualWeightAllocations(scores []Score) ([]portfolio.Allocation, error) {
	if len(scores) == 0 {
		return nil, fmt.Errorf("at least one selected score is required")
	}
	weight := decimal.NewFromInt(1).DivRound(decimal.NewFromInt(int64(len(scores))), decimalScale)
	allocations := make([]portfolio.Allocation, 0, len(scores))
	seen := make(map[string]struct{}, len(scores))
	allocated := decimal.Zero
	for index, score := range scores {
		symbol := strings.ToUpper(strings.TrimSpace(score.Symbol))
		if symbol == "" {
			return nil, fmt.Errorf("selected score symbol must not be empty")
		}
		if _, exists := seen[symbol]; exists {
			return nil, fmt.Errorf("selected scores contain duplicate symbol %s", symbol)
		}
		seen[symbol] = struct{}{}
		currentWeight := weight
		if index == len(scores)-1 {
			currentWeight = decimal.NewFromInt(1).Sub(allocated)
		}
		allocation, err := portfolio.NewAllocation(symbol, currentWeight)
		if err != nil {
			return nil, fmt.Errorf("create allocation for %s: %w", symbol, err)
		}
		allocations = append(allocations, allocation)
		allocated = allocated.Add(currentWeight)
	}
	return allocations, nil
}

func wilderATR(bars []marketdata.MonthlyBar, period int) (decimal.Decimal, error) {
	if len(bars) < period+1 {
		return decimal.Zero, fmt.Errorf("requires %d bars", period+1)
	}
	trueRanges := make([]decimal.Decimal, 0, len(bars)-1)
	for index := 1; index < len(bars); index++ {
		previousClose := bars[index-1].Close
		bar := bars[index]
		trueRange := bar.High.Sub(bar.Low)
		if gap := bar.High.Sub(previousClose).Abs(); gap.GreaterThan(trueRange) {
			trueRange = gap
		}
		if gap := bar.Low.Sub(previousClose).Abs(); gap.GreaterThan(trueRange) {
			trueRange = gap
		}
		trueRanges = append(trueRanges, trueRange)
	}

	atr := decimal.Zero
	for _, trueRange := range trueRanges[:period] {
		atr = atr.Add(trueRange)
	}
	atr = atr.DivRound(decimal.NewFromInt(int64(period)), decimalScale)
	for _, trueRange := range trueRanges[period:] {
		atr = atr.Mul(decimal.NewFromInt(int64(period-1))).Add(trueRange).DivRound(decimal.NewFromInt(int64(period)), decimalScale)
	}
	return atr, nil
}

func validBar(bar marketdata.MonthlyBar) bool {
	if !bar.Open.IsPositive() || !bar.High.IsPositive() || !bar.Low.IsPositive() || !bar.Close.IsPositive() {
		return false
	}
	return !bar.High.LessThan(bar.Open) && !bar.High.LessThan(bar.Close) && !bar.High.LessThan(bar.Low) && !bar.Low.GreaterThan(bar.Open) && !bar.Low.GreaterThan(bar.Close)
}
