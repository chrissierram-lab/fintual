package universe_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"portfolio/universe"
)

func TestLoadCSV(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "normalizes tickers and accepts header", input: "symbol\n meta \nAAPL\nBRK-B\n", want: []string{"META", "AAPL", "BRK-B"}},
		{name: "accepts ticker header and skips blanks", input: "ticker\n\nMSFT\n", want: []string{"MSFT"}},
		{name: "rejects duplicates after normalization", input: "symbol\nMETA\n meta \n", wantErr: true},
		{name: "rejects invalid ticker", input: "META/OTHER\n", wantErr: true},
		{name: "rejects multiple columns", input: "META,Technology\n", wantErr: true},
		{name: "rejects empty universe", input: "symbol\n\n", wantErr: true},
		{name: "rejects malformed CSV", input: "\"META\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := universe.LoadCSV(strings.NewReader(test.input))
			if test.wantErr {
				if err == nil {
					t.Fatal("LoadCSV() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadCSV() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("LoadCSV() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestLoadCSVFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "universe.csv")
	if err := os.WriteFile(path, []byte("symbol\nMETA\n"), 0o600); err != nil {
		t.Fatalf("write universe file: %v", err)
	}
	got, err := universe.LoadCSVFile(path)
	if err != nil {
		t.Fatalf("LoadCSVFile() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"META"}) {
		t.Fatalf("LoadCSVFile() = %#v, want [META]", got)
	}
	if _, err := universe.LoadCSVFile(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Fatal("LoadCSVFile() error = nil for missing file")
	}
}
