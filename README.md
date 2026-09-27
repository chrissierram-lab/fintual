# Portfolio rebalance simulator

A small Go application that values a portfolio using Yahoo Finance quotes and
prints a rebalance plan. It is a **dry-run tool**: it does not connect to a
broker or execute orders.

## Run

```sh
go test ./...
go run ./cmd/portfolio
```

The default command reads trading-cost assumptions from `config/config.json`
and a synthetic holdings example from `config/portfolio.example.json`. Override
either path with `-config` or `-portfolio`. Use a private local portfolio file
for real holdings; do not commit personal account data.

```sh
go run ./cmd/portfolio -config config/config.json -portfolio /path/to/my-portfolio.json
```

The JSON report includes quote timestamps, suggested orders, gross values,
commissions, estimated taxes, net cash flow, additional cash required, and any
rounding residual. The command fails without emitting a partial report if quote
retrieval or portfolio validation fails.

## Monthly momentum strategy

Run this mode manually once per month after the monthly bar has closed:

```sh
go run ./cmd/portfolio \
	-mode momentum \
	-portfolio config/holdings.example.json \
	-universe config/universe.example.csv \
	-top 3
```

The CSV is the candidate universe, with one ticker per row and an optional
`symbol` or `ticker` header. For every candidate the app requests two years of
monthly OHLC data from Yahoo, then excludes the current, potentially incomplete
month. At least 15 completed monthly bars are required for every candidate; a
missing/invalid history aborts the whole calculation so the ranking is never
silently based on a smaller universe.

The score follows the supplied ProRealTime formula, interpreting ROC as percent
change and ATR as Wilder-smoothed true range:

$$
ROC_n = 100\left(\frac{Close_t}{Close_{t-n}}-1\right),\qquad
F = \frac{0.4\,ROC_8 + 0.2\,ROC_{10}}
				 {0.4\,(ATR_{14}/SMA_{14}(Close))}
$$

Candidates are ranked by `F` descending; equal scores break alphabetically by
ticker. The top `-top` assets receive equal target weights. Since one third is
repeating in base 10, targets use 18 decimal places and the final asset absorbs
the exact-sum remainder. There is no positive-score filter: if all scores are
negative, the mode still selects the top three, as requested. Holdings not in
the selected set are fully sold; therefore any holding that may be sold needs
FIFO acquisition lots for the tax estimate.

The command is still a dry run and is manually invoked; it does not schedule
itself, persist a “already rebalanced this month” marker, or place broker
orders. It uses the latest available quotes to size trades, which may be newer
than the completed monthly bars used to rank assets. Wilder smoothing is the
ATR interpretation used here; compare a known ticker/month against ProRealTime
before treating the score as identical to its built-in indicator.

## Input and assumptions

Amounts, share quantities, acquisition costs, weights, commissions, and tax
rates are JSON strings parsed as exact base-10 decimals. Positions accept either
acquisition lots or a total quantity. Lots enable FIFO cost-basis estimates;
a position without lots cannot be sold when a sale-tax estimate is required.
Each allocation is a fraction, so `"0.4"` means 40%, and allocations must total
exactly one.

The example assumes USD, a fixed commission per order, and an effective tax
estimate on positive realized gain after the sell commission. The tax rate and
commission in the sample are illustrative, not verified broker fees or Chilean
tax rules. The simulator has no cash balance, uses the current gross value as
the target base, and rounds partial share quantities down to eight decimal
places. See [configuration notes](config/README.md).

Yahoo Finance's chart endpoint is unofficial and may delay or limit data. Check
quote timestamps before relying on a plan. No recommendation from this exercise
is financial or tax advice.

## AI assistance

GitHub Copilot was used iteratively to design and implement the domain model,
decimal arithmetic, FIFO cost basis, estimated trading costs, Yahoo quote
adapter, and dry-run command. The Copilot conversation is the source discussion
for those design decisions; attach or export the conversation itself when
submitting this exercise if the assignment requires the full chat transcript.