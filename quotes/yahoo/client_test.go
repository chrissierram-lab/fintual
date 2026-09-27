package yahoo_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"portfolio/quotes/yahoo"
)

func TestClient_Quote(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		symbol         string
		wantPrice      string
		wantTimestamp  time.Time
		wantError      bool
		wantStatusCode int
	}{
		{
			name:          "parses exact regular market price and timestamp",
			body:          chartPayload("META", "USD", "123.45000001", 1700000000, []int64{1700000000}, []string{"123.45"}),
			symbol:        " meta ",
			wantPrice:     "123.45000001",
			wantTimestamp: time.Unix(1700000000, 0).UTC(),
		},
		{
			name:          "parses a decimal encoded as a JSON string",
			body:          chartPayload("META", "USD", `"123.4500"`, 1700000000, []int64{1700000000}, []string{"123.45"}),
			symbol:        "META",
			wantPrice:     "123.45",
			wantTimestamp: time.Unix(1700000000, 0).UTC(),
		},
		{
			name:          "falls back to the latest non-null close",
			body:          chartPayload("META", "USD", "null", 0, []int64{1000, 2000, 3000}, []string{"10", "null", "12.3456789"}),
			symbol:        "META",
			wantPrice:     "12.3456789",
			wantTimestamp: time.Unix(3000, 0).UTC(),
		},
		{name: "rejects malformed JSON", body: "{", symbol: "META", wantError: true},
		{name: "rejects empty chart result", body: `{"chart":{"result":[],"error":null}}`, symbol: "META", wantError: true},
		{name: "surfaces Yahoo chart errors", body: `{"chart":{"result":null,"error":{"code":"Not Found","description":"No data found"}}}`, symbol: "META", wantError: true},
		{name: "falls back to Yahoo error code when description is missing", body: `{"chart":{"result":null,"error":{"code":"Not Found","description":""}}}`, symbol: "META", wantError: true},
		{name: "rejects a mismatched response symbol", body: chartPayload("AAPL", "USD", "123.45", 1700000000, []int64{1700000000}, []string{"123.45"}), symbol: "META", wantError: true},
		{name: "rejects non-USD quotes", body: chartPayload("META", "EUR", "123.45", 1700000000, []int64{1700000000}, []string{"123.45"}), symbol: "META", wantError: true},
		{name: "rejects a response without a usable price", body: chartPayload("META", "USD", "null", 0, []int64{0}, []string{"null"}), symbol: "META", wantError: true},
		{name: "reports non-success HTTP status", statusCode: http.StatusTooManyRequests, body: "rate limited", symbol: "META", wantError: true, wantStatusCode: http.StatusTooManyRequests},
		{name: "rejects oversized response", body: strings.Repeat("x", 1<<20+1), symbol: "META", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statusCode := test.statusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			client := testClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(statusCode)
				_, _ = writer.Write([]byte(test.body))
			})

			stock, err := client.Quote(context.Background(), test.symbol)
			if test.wantError {
				var quoteError *yahoo.QuoteError
				if !errors.As(err, &quoteError) {
					t.Fatalf("Quote() error = %v, want *yahoo.QuoteError", err)
				}
				if quoteError.StatusCode != test.wantStatusCode {
					t.Fatalf("QuoteError.StatusCode = %d, want %d", quoteError.StatusCode, test.wantStatusCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Quote() error = %v", err)
			}
			if stock.Symbol() != "META" {
				t.Fatalf("Symbol() = %q, want META", stock.Symbol())
			}
			if got := stock.CurrentPrice().String(); got != test.wantPrice {
				t.Fatalf("CurrentPrice() = %q, want %q", got, test.wantPrice)
			}
			if !stock.PriceUpdatedAt().Equal(test.wantTimestamp) {
				t.Fatalf("PriceUpdatedAt() = %s, want %s", stock.PriceUpdatedAt(), test.wantTimestamp)
			}
		})
	}
}

func TestClient_QuoteBuildsRequest(t *testing.T) {
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("HTTP method = %q, want GET", request.Method)
		}
		if request.URL.Path != "/v8/finance/chart/META" {
			t.Errorf("URL path = %q, want chart path for META", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("range") != "1d" || query.Get("interval") != "1d" {
			t.Errorf("query = %v, want range=1d and interval=1d", query)
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", request.Header.Get("Accept"))
		}
		if request.Header.Get("User-Agent") == "" {
			t.Error("User-Agent is empty")
		}
		_, _ = writer.Write([]byte(chartPayload("META", "USD", "100", 1700000000, []int64{1700000000}, []string{"100"})))
	})

	if _, err := client.Quote(context.Background(), "META"); err != nil {
		t.Fatalf("Quote() error = %v", err)
	}
}

func TestClient_MonthlyHistory(t *testing.T) {
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("range") != "2y" || query.Get("interval") != "1mo" {
			t.Errorf("query = %v, want range=2y and interval=1mo", query)
		}
		_, _ = writer.Write([]byte(`{"chart":{"result":[{"meta":{"currency":"USD","symbol":"META"},"timestamp":[1735689600,1738368000],"indicators":{"quote":[{"open":["10.00000001",null],"high":["12",null],"low":["9",null],"close":["11.12345678",null]}]}}],"error":null}}`))
	})

	history, err := client.MonthlyHistory(context.Background(), "meta")
	if err != nil {
		t.Fatalf("MonthlyHistory() error = %v", err)
	}
	if history.Symbol != "META" || history.Currency != "USD" || len(history.Bars) != 2 {
		t.Fatalf("MonthlyHistory() = %#v, want two timestamped META/USD bars", history)
	}
	bar := history.Bars[0]
	if got := bar.Open.String(); got != "10.00000001" {
		t.Fatalf("Open = %q, want exact decimal", got)
	}
	if got := bar.High.String(); got != "12" {
		t.Fatalf("High = %q, want 12", got)
	}
	if got := bar.Low.String(); got != "9" {
		t.Fatalf("Low = %q, want 9", got)
	}
	if got := bar.Close.String(); got != "11.12345678" {
		t.Fatalf("Close = %q, want exact decimal", got)
	}
	if history.Bars[1].Close.IsPositive() {
		t.Fatalf("null OHLC row was silently discarded or treated as positive: %#v", history.Bars[1])
	}
}

func TestClient_MonthlyHistoryRejectsWrongCurrencyAndEmptyBars(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "wrong currency", body: `{"chart":{"result":[{"meta":{"currency":"EUR","symbol":"META"},"timestamp":[1735689600],"indicators":{"quote":[{"open":["10"],"high":["12"],"low":["9"],"close":["11"]}]}}],"error":null}}`},
		{name: "no bars", body: `{"chart":{"result":[{"meta":{"currency":"USD","symbol":"META"},"timestamp":[],"indicators":{"quote":[]}}],"error":null}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := testClient(t, func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.body))
			})
			if _, err := client.MonthlyHistory(context.Background(), "META"); err == nil {
				t.Fatal("MonthlyHistory() error = nil, want error")
			}
		})
	}
}

func TestClient_QuotesDeduplicatesAndPreservesOrder(t *testing.T) {
	requestCount := 0
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		requestCount++
		symbol := strings.TrimPrefix(request.URL.Path, "/v8/finance/chart/")
		_, _ = writer.Write([]byte(chartPayload(symbol, "USD", "100.25", 1700000000, []int64{1700000000}, []string{"100.25"})))
	})

	quotes, err := client.Quotes(context.Background(), []string{" meta ", "META", "aapl"})
	if err != nil {
		t.Fatalf("Quotes() error = %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2 after deduplicating META", requestCount)
	}
	if len(quotes) != 2 || quotes[0].Symbol() != "META" || quotes[1].Symbol() != "AAPL" {
		t.Fatalf("Quotes() symbols = %#v, want [META AAPL]", quotes)
	}
}

func TestClient_QuotesReturnsNoPartialResults(t *testing.T) {
	client := testClient(t, func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/AAPL") {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte(chartPayload("META", "USD", "100", 1700000000, []int64{1700000000}, []string{"100"})))
	})

	quotes, err := client.Quotes(context.Background(), []string{"META", "AAPL"})
	if err == nil {
		t.Fatal("Quotes() error = nil, want error")
	}
	if quotes != nil {
		t.Fatalf("Quotes() = %#v, want nil on partial failure", quotes)
	}
}

func TestClient_RejectsInvalidSymbolsAndBaseURLs(t *testing.T) {
	client := yahoo.NewClient()
	tests := []struct {
		name   string
		symbol string
	}{
		{name: "empty symbol", symbol: " "},
		{name: "path separators are not allowed", symbol: "META/OTHER"},
		{name: "query characters are not allowed", symbol: "META?range=max"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.Quote(context.Background(), test.symbol)
			var quoteError *yahoo.QuoteError
			if !errors.As(err, &quoteError) {
				t.Fatalf("Quote() error = %v, want *yahoo.QuoteError", err)
			}
		})
	}

	for _, baseURL := range []string{"", "ftp://finance.example", "/relative/path", "https://finance.example/path?query=1", "https://finance.example/path#fragment"} {
		if _, err := yahoo.NewClientWithBaseURL(baseURL, nil); err == nil {
			t.Errorf("NewClientWithBaseURL(%q) error = nil, want an error", baseURL)
		}
	}
	if _, err := yahoo.NewClientWithBaseURL("http://[", nil); err == nil {
		t.Fatal("NewClientWithBaseURL() error = nil for malformed URL")
	}
}

func TestClient_QuotesRejectsEmptyInput(t *testing.T) {
	client := yahoo.NewClient()
	quotes, err := client.Quotes(context.Background(), nil)
	if err == nil {
		t.Fatal("Quotes() error = nil, want an error")
	}
	if quotes != nil {
		t.Fatalf("Quotes() = %#v, want nil", quotes)
	}
}

func TestClient_QuotesRejectsInvalidSymbol(t *testing.T) {
	client := yahoo.NewClient()
	quotes, err := client.Quotes(context.Background(), []string{"META", "AAPL/OTHER"})
	var quoteError *yahoo.QuoteError
	if !errors.As(err, &quoteError) {
		t.Fatalf("Quotes() error = %v, want *yahoo.QuoteError", err)
	}
	if quotes != nil {
		t.Fatalf("Quotes() = %#v, want nil", quotes)
	}
}

func TestClient_QuoteWrapsTransportAndBodyReadErrors(t *testing.T) {
	tests := []struct {
		name      string
		transport roundTripFunc
		wantCause error
	}{
		{
			name: "transport error",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
			wantCause: context.DeadlineExceeded,
		},
		{
			name: "body read error",
			transport: func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       failingReadCloser{err: io.ErrUnexpectedEOF},
					Request:    request,
				}, nil
			},
			wantCause: io.ErrUnexpectedEOF,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: test.transport}
			client, err := yahoo.NewClientWithBaseURL("http://finance.example", httpClient)
			if err != nil {
				t.Fatalf("NewClientWithBaseURL() error = %v", err)
			}
			_, err = client.Quote(context.Background(), "META")
			if !errors.Is(err, test.wantCause) {
				t.Fatalf("Quote() error = %v, want cause %v", err, test.wantCause)
			}
		})
	}
}

func TestQuoteError_ErrorAndUnwrap(t *testing.T) {
	underlying := errors.New("network unavailable")
	tests := []struct {
		name      string
		error     *yahoo.QuoteError
		wantText  string
		wantCause error
	}{
		{name: "empty cause", error: &yahoo.QuoteError{Symbol: "META"}, wantText: "Yahoo quote for META failed"},
		{name: "HTTP status", error: &yahoo.QuoteError{Symbol: "META", StatusCode: http.StatusBadGateway}, wantText: "Yahoo quote for META returned HTTP 502"},
		{name: "wrapped cause", error: &yahoo.QuoteError{Symbol: "META", Cause: underlying}, wantText: "Yahoo quote for META: network unavailable", wantCause: underlying},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.error.Error(); got != test.wantText {
				t.Fatalf("Error() = %q, want %q", got, test.wantText)
			}
			if test.wantCause == nil {
				if test.error.Unwrap() != nil {
					t.Fatalf("Unwrap() = %v, want nil", test.error.Unwrap())
				}
			} else if !errors.Is(test.error, test.wantCause) {
				t.Fatalf("errors.Is(error, cause) = false, want true")
			}
		})
	}
}

func TestQuoteErrorUnwrapsCause(t *testing.T) {
	client := testClient(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("{"))
	})

	_, err := client.Quote(context.Background(), "META")
	var quoteError *yahoo.QuoteError
	if !errors.As(err, &quoteError) {
		t.Fatalf("error = %v, want *yahoo.QuoteError", err)
	}
	if quoteError.Cause == nil {
		t.Fatal("QuoteError.Cause = nil, want parse error")
	}
}

func testClient(t *testing.T, handler http.HandlerFunc) *yahoo.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := yahoo.NewClientWithBaseURL(server.URL+"/v8/finance/chart", server.Client())
	if err != nil {
		t.Fatalf("NewClientWithBaseURL() error = %v", err)
	}
	return client
}

func chartPayload(symbol, currency, regularPrice string, regularTime int64, timestamps []int64, closes []string) string {
	return fmt.Sprintf(`{"chart":{"result":[{"meta":{"currency":%q,"symbol":%q,"regularMarketTime":%d,"regularMarketPrice":%s},"timestamp":%s,"indicators":{"quote":[{"close":[%s]}]}}],"error":null}}`,
		currency,
		symbol,
		regularTime,
		regularPrice,
		jsonInt64Slice(timestamps),
		strings.Join(closes, ","),
	)
}

func jsonInt64Slice(values []int64) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprintf("%d", value)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type failingReadCloser struct {
	err error
}

func (r failingReadCloser) Read([]byte) (int, error) { return 0, r.err }

func (failingReadCloser) Close() error { return nil }
