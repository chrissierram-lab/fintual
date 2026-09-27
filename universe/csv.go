package universe

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxUniverseFileBytes = 1 << 20

// LoadCSVFile loads unique ticker symbols from a one-column CSV file.
func LoadCSVFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open universe CSV: %w", err)
	}
	defer file.Close()
	return LoadCSV(file)
}

// LoadCSV reads one ticker per row and accepts an optional SYMBOL or TICKER header.
func LoadCSV(reader io.Reader) ([]string, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxUniverseFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read universe CSV: %w", err)
	}
	if len(contents) > maxUniverseFileBytes {
		return nil, fmt.Errorf("universe CSV exceeds %d bytes", maxUniverseFileBytes)
	}

	csvReader := csv.NewReader(strings.NewReader(string(contents)))
	csvReader.TrimLeadingSpace = true
	csvReader.FieldsPerRecord = -1

	symbols := make([]string, 0)
	seen := make(map[string]struct{})
	rowNumber := 0
	for {
		record, readErr := csvReader.Read()
		if readErr == io.EOF {
			break
		}
		rowNumber++
		if readErr != nil {
			return nil, fmt.Errorf("read universe CSV row %d: %w", rowNumber, readErr)
		}
		if len(record) != 1 {
			return nil, fmt.Errorf("universe CSV row %d must have exactly one column", rowNumber)
		}
		symbol := strings.ToUpper(strings.TrimSpace(record[0]))
		if symbol == "" {
			continue
		}
		if rowNumber == 1 && (symbol == "SYMBOL" || symbol == "TICKER") {
			continue
		}
		if !validSymbol(symbol) {
			return nil, fmt.Errorf("universe CSV row %d contains invalid symbol %q", rowNumber, record[0])
		}
		if _, exists := seen[symbol]; exists {
			return nil, fmt.Errorf("universe CSV contains duplicate symbol %s", symbol)
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	if len(symbols) == 0 {
		return nil, fmt.Errorf("universe CSV contains no symbols")
	}
	return symbols, nil
}

func validSymbol(symbol string) bool {
	for _, character := range symbol {
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune(".^-=", character) {
			continue
		}
		return false
	}
	return true
}

//
