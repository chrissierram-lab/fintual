package marketdata

import (
	"time"

	"github.com/shopspring/decimal"
)

// MonthlyBar is one monthly OHLC observation. Time is the Yahoo chart
// timestamp for the bar, represented in UTC.
type MonthlyBar struct {
	Time  time.Time
	Open  decimal.Decimal
	High  decimal.Decimal
	Low   decimal.Decimal
	Close decimal.Decimal
}

// MonthlyHistory groups monthly bars for one instrument.
type MonthlyHistory struct {
	Symbol   string
	Currency string
	Bars     []MonthlyBar
}
