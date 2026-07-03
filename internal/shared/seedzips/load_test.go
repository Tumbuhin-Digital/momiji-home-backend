package seedzips

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadZipRowsFromCSV_includes94104SanFrancisco(t *testing.T) {
	csvPath := filepath.Join("..", "..", "..", "data", "uszips.csv")
	rows, err := LoadZipRowsFromCSV(csvPath)
	if err != nil {
		t.Fatalf("load csv: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected parsed rows")
	}

	var found bool
	for _, row := range rows {
		if row.ZipCode != "94104" {
			continue
		}
		found = true
		if !strings.EqualFold(row.City, "San Francisco") {
			t.Fatalf("94104 city = %q, want San Francisco", row.City)
		}
		if row.StateAbbr != "CA" {
			t.Fatalf("94104 state = %q, want CA", row.StateAbbr)
		}
		break
	}
	if !found {
		t.Fatal("94104 not found in parsed CSV rows")
	}
}
