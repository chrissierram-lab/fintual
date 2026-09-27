# Yahoo Finance quotes

This adapter reads Yahoo Finance's publicly accessible chart endpoint and
converts `regularMarketPrice` and `regularMarketTime` into portfolio quotes. If
the regular-market fields are unavailable, it falls back to the last valid
chart close and its timestamp. Numeric JSON is parsed directly as a decimal.

```go
quoteClient := yahoo.NewClient()
quotes, err := quoteClient.Quotes(ctx, []string{"META", "AAPL"})
if err != nil {
	return err
}

plan, err := holdings.Rebalance(
	quotes,
	settings.FixedCommissionPerOrder(),
	settings.EstimatedTaxRate(),
)
```

This endpoint is unofficial, is not a guaranteed real-time market-data service,
and may change, delay data, or rate-limit requests. The adapter accepts USD
quotes only because the portfolio model currently has no multi-currency support.
It fetches unique symbols sequentially and returns no partial batch if any quote
fails. Check each quote timestamp before relying on the resulting rebalance plan.

`MonthlyHistory(ctx, symbol)` requests two years of monthly bars with OHLC
fields. It preserves timestamped rows with missing values so the strategy can
ignore an incomplete current-month candle while rejecting malformed completed
months instead of silently changing the lookback window.