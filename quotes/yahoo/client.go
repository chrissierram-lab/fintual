package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"portfolio"
	"portfolio/marketdata"
)

const (
	defaultBaseURL    = "https://query1.finance.yahoo.com/v8/finance/chart"
	defaultTimeout    = 10 * time.Second
	maxResponseBytes  = 1 << 20
	supportedCurrency = "USD"
)

// Client fetches quotes from Yahoo Finance's chart endpoint. The endpoint is
// not an official stable API and may change or rate-limit callers.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// QuoteError describes a failed quote request and preserves its underlying error.
type QuoteError struct {
	Symbol     string
	StatusCode int
	Cause      error
}

func (e *QuoteError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("Yahoo quote for %s returned HTTP %d", e.Symbol, e.StatusCode)
	}
	if e.Cause != nil {
		return fmt.Sprintf("Yahoo quote for %s: %v", e.Symbol, e.Cause)
	}
	return fmt.Sprintf("Yahoo quote for %s failed", e.Symbol)
}

func (e *QuoteError) Unwrap() error { return e.Cause }

type chartResponse struct {
	Chart struct {
		Result []chartResult  `json:"result"`
		Error  *chartAPIError `json:"error"`
	} `json:"chart"`
}

type chartResult struct {
	Meta struct {
		Currency           string          `json:"currency"`
		Symbol             string          `json:"symbol"`
		RegularMarketTime  int64           `json:"regularMarketTime"`
		RegularMarketPrice json.RawMessage `json:"regularMarketPrice"`
	} `json:"meta"`
	Timestamp  []int64 `json:"timestamp"`
	Indicators struct {
		Quote []struct {
			Open  []json.RawMessage `json:"open"`
			High  []json.RawMessage `json:"high"`
			Low   []json.RawMessage `json:"low"`
			Close []json.RawMessage `json:"close"`
		} `json:"quote"`
	} `json:"indicators"`
}

type chartAPIError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

// NewClient creates a Yahoo chart client with a ten-second request timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
	}
}

// NewClientWithBaseURL creates a client with an injectable HTTP client and
// endpoint, which is useful for controlled deployments and httptest servers.
func NewClientWithBaseURL(baseURL string, httpClient *http.Client) (*Client, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("Yahoo base URL must be an absolute HTTP(S) URL without query or fragment")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(parsedURL.String(), "/"),
	}, nil
}

// Quote returns the latest available USD quote for one symbol. ctx must not be nil.
func (c *Client) Quote(ctx context.Context, symbol string) (portfolio.Stock, error) {
	normalizedSymbol, err := normalizeSymbol(symbol)
	if err != nil {
		return portfolio.Stock{}, &QuoteError{Symbol: symbol, Cause: err}
	}
	result, err := c.fetchChart(ctx, normalizedSymbol, "1d", "1d")
	if err != nil {
		return portfolio.Stock{}, err
	}

	price, quoteTime, err := latestPrice(result)
	if err != nil {
		return portfolio.Stock{}, &QuoteError{Symbol: normalizedSymbol, Cause: err}
	}
	stock, err := portfolio.NewStock(normalizedSymbol, price, quoteTime)
	if err != nil {
		return portfolio.Stock{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("invalid quote: %w", err)}
	}
	return stock, nil
}

// MonthlyHistory returns Yahoo's monthly OHLC bars for the last two years.
// It validates currency and symbol but leaves completed-month filtering to
// the momentum calculator so that its as-of date remains deterministic.
func (c *Client) MonthlyHistory(ctx context.Context, symbol string) (marketdata.MonthlyHistory, error) {
	normalizedSymbol, err := normalizeSymbol(symbol)
	if err != nil {
		return marketdata.MonthlyHistory{}, &QuoteError{Symbol: symbol, Cause: err}
	}
	result, err := c.fetchChart(ctx, normalizedSymbol, "2y", "1mo")
	if err != nil {
		return marketdata.MonthlyHistory{}, err
	}
	if len(result.Indicators.Quote) == 0 {
		return marketdata.MonthlyHistory{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("Yahoo returned no monthly OHLC bars")}
	}

	series := result.Indicators.Quote[0]
	barCount := len(result.Timestamp)
	for _, values := range [][]json.RawMessage{series.Open, series.High, series.Low, series.Close} {
		if len(values) < barCount {
			barCount = len(values)
		}
	}
	if barCount == 0 {
		return marketdata.MonthlyHistory{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("Yahoo returned no monthly OHLC bars")}
	}

	bars := make([]marketdata.MonthlyBar, 0, barCount)
	for index := 0; index < barCount; index++ {
		if result.Timestamp[index] <= 0 {
			continue
		}
		open, _ := parsePositiveDecimal(series.Open[index])
		high, _ := parsePositiveDecimal(series.High[index])
		low, _ := parsePositiveDecimal(series.Low[index])
		closePrice, _ := parsePositiveDecimal(series.Close[index])
		bars = append(bars, marketdata.MonthlyBar{
			Time:  time.Unix(result.Timestamp[index], 0).UTC(),
			Open:  open,
			High:  high,
			Low:   low,
			Close: closePrice,
		})
	}
	if len(bars) == 0 {
		return marketdata.MonthlyHistory{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("Yahoo returned no valid monthly OHLC bars")}
	}
	return marketdata.MonthlyHistory{Symbol: normalizedSymbol, Currency: supportedCurrency, Bars: bars}, nil
}

func (c *Client) fetchChart(ctx context.Context, normalizedSymbol, dateRange, interval string) (chartResult, error) {
	requestURL, err := url.Parse(c.baseURL + "/" + url.PathEscape(normalizedSymbol))
	if err != nil {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("build request URL: %w", err)}
	}
	query := requestURL.Query()
	query.Set("range", dateRange)
	query.Set("interval", interval)
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("create request: %w", err)}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "portfolio-rebalancer/1.0")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: err}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("read response: %w", err)}
	}
	if len(body) > maxResponseBytes {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("response exceeds %d bytes", maxResponseBytes)}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, StatusCode: response.StatusCode}
	}

	var payload chartResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("decode response: %w", err)}
	}
	if payload.Chart.Error != nil {
		description := strings.TrimSpace(payload.Chart.Error.Description)
		if description == "" {
			description = strings.TrimSpace(payload.Chart.Error.Code)
		}
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("Yahoo chart error: %s", description)}
	}
	if len(payload.Chart.Result) == 0 {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("Yahoo returned no chart result")}
	}

	result := payload.Chart.Result[0]
	returnedSymbol, err := normalizeSymbol(result.Meta.Symbol)
	if err != nil || returnedSymbol != normalizedSymbol {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("Yahoo returned a different or missing symbol")}
	}
	if !strings.EqualFold(result.Meta.Currency, supportedCurrency) {
		return chartResult{}, &QuoteError{Symbol: normalizedSymbol, Cause: fmt.Errorf("quote currency must be %s", supportedCurrency)}
	}
	return result, nil
}

// Quotes returns unique symbols in input order. It returns no partial result if
// any request fails, so a caller cannot accidentally rebalance on incomplete data.
func (c *Client) Quotes(ctx context.Context, symbols []string) ([]portfolio.Stock, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("at least one symbol is required")
	}

	quotes := make([]portfolio.Stock, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		normalizedSymbol, err := normalizeSymbol(symbol)
		if err != nil {
			return nil, &QuoteError{Symbol: symbol, Cause: err}
		}
		if _, exists := seen[normalizedSymbol]; exists {
			continue
		}
		seen[normalizedSymbol] = struct{}{}

		quote, err := c.Quote(ctx, normalizedSymbol)
		if err != nil {
			return nil, err
		}
		quotes = append(quotes, quote)
	}
	return quotes, nil
}

func normalizeSymbol(symbol string) (string, error) {
	normalizedSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	if normalizedSymbol == "" {
		return "", fmt.Errorf("symbol must not be empty")
	}
	for _, character := range normalizedSymbol {
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune(".^-=", character) {
			continue
		}
		return "", fmt.Errorf("symbol contains unsupported characters")
	}
	return normalizedSymbol, nil
}

func latestPrice(result chartResult) (decimal.Decimal, time.Time, error) {
	if result.Meta.RegularMarketTime > 0 {
		price, err := parsePositiveDecimal(result.Meta.RegularMarketPrice)
		if err == nil {
			return price, time.Unix(result.Meta.RegularMarketTime, 0).UTC(), nil
		}
	}

	if len(result.Indicators.Quote) > 0 {
		closes := result.Indicators.Quote[0].Close
		limit := len(closes)
		if len(result.Timestamp) < limit {
			limit = len(result.Timestamp)
		}
		for index := limit - 1; index >= 0; index-- {
			if result.Timestamp[index] <= 0 {
				continue
			}
			price, err := parsePositiveDecimal(closes[index])
			if err == nil {
				return price, time.Unix(result.Timestamp[index], 0).UTC(), nil
			}
		}
	}
	return decimal.Zero, time.Time{}, fmt.Errorf("Yahoo response has no valid price and timestamp")
}

func parsePositiveDecimal(raw json.RawMessage) (decimal.Decimal, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return decimal.Zero, fmt.Errorf("price is missing")
	}
	if strings.HasPrefix(value, "\"") {
		if err := json.Unmarshal(raw, &value); err != nil {
			return decimal.Zero, fmt.Errorf("decode price: %w", err)
		}
	}
	price, err := decimal.NewFromString(value)
	if err != nil || !price.IsPositive() {
		return decimal.Zero, fmt.Errorf("price must be a positive decimal")
	}
	return price, nil
}
