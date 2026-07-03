package seedzips

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/checkout"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/uszip"
)

var usStateNames = map[string]string{
	"AL": "Alabama", "AK": "Alaska", "AZ": "Arizona", "AR": "Arkansas", "CA": "California",
	"CO": "Colorado", "CT": "Connecticut", "DE": "Delaware", "FL": "Florida", "GA": "Georgia",
	"HI": "Hawaii", "ID": "Idaho", "IL": "Illinois", "IN": "Indiana", "IA": "Iowa",
	"KS": "Kansas", "KY": "Kentucky", "LA": "Louisiana", "ME": "Maine", "MD": "Maryland",
	"MA": "Massachusetts", "MI": "Michigan", "MN": "Minnesota", "MS": "Mississippi", "MO": "Missouri",
	"MT": "Montana", "NE": "Nebraska", "NV": "Nevada", "NH": "New Hampshire", "NJ": "New Jersey",
	"NM": "New Mexico", "NY": "New York", "NC": "North Carolina", "ND": "North Dakota", "OH": "Ohio",
	"OK": "Oklahoma", "OR": "Oregon", "PA": "Pennsylvania", "RI": "Rhode Island", "SC": "South Carolina",
	"SD": "South Dakota", "TN": "Tennessee", "TX": "Texas", "UT": "Utah", "VT": "Vermont",
	"VA": "Virginia", "WA": "Washington", "WV": "West Virginia", "WI": "Wisconsin", "WY": "Wyoming",
	"DC": "District of Columbia", "PR": "Puerto Rico",
}

func LoadZipRowsFromCSV(path string) ([]checkout.UsZipCode, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return LoadZipRowsFromReader(file)
}

func LoadZipRowsFromReader(r io.Reader) ([]checkout.UsZipCode, error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	col := indexColumns(header)
	byZip := make(map[string]checkout.UsZipCode)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		zipRaw := field(record, col, "zip", "zipcode", "zip_code", "postal", "postal_code")
		city := field(record, col, "city", "primary_city")
		stateAbbr := strings.ToUpper(field(record, col, "state_id", "state_abbr", "state", "state_code"))
		stateName := field(record, col, "state_name", "state")

		zipCode, ok := uszip.NormalizeUSZip(zipRaw)
		if !ok || city == "" || stateAbbr == "" {
			continue
		}
		if len(stateAbbr) > 2 {
			continue
		}
		if name, ok := usStateNames[stateAbbr]; ok && (stateName == "" || len(stateName) == 2) {
			stateName = name
		}
		if stateName == "" {
			stateName = stateAbbr
		}

		if _, exists := byZip[zipCode]; exists {
			continue
		}

		byZip[zipCode] = checkout.UsZipCode{
			ZipCode:   zipCode,
			City:      city,
			StateAbbr: stateAbbr,
			StateName: stateName,
		}
	}

	rows := make([]checkout.UsZipCode, 0, len(byZip))
	for _, row := range byZip {
		rows = append(rows, row)
	}
	return rows, nil
}

func indexColumns(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for i, name := range header {
		key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
		index[key] = i
	}
	return index
}

func field(record []string, col map[string]int, names ...string) string {
	for _, name := range names {
		if idx, ok := col[name]; ok && idx < len(record) {
			value := strings.TrimSpace(record[idx])
			if value != "" {
				return value
			}
		}
	}
	return ""
}
