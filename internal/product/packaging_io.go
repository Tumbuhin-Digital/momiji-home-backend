package product

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
	"github.com/xuri/excelize/v2"
)

var packagingTemplateHeaders = []string{
	"variant_id",
	"product_title",
	"variant_title",
	"sku",
	"weight_lb",
	"length_in",
	"width_in",
	"height_in",
}

func buildPackagingTemplateExcel(variants []ProductVariant) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheetName := "Packaging"
	if err := f.SetSheetName("Sheet1", sheetName); err != nil {
		return nil, fmt.Errorf("rename sheet: %w", err)
	}

	for i, h := range packagingTemplateHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheetName, cell, h); err != nil {
			return nil, fmt.Errorf("write header: %w", err)
		}
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
	})
	if err != nil {
		return nil, fmt.Errorf("header style: %w", err)
	}
	if err := f.SetCellStyle(sheetName, "A1", "H1", headerStyle); err != nil {
		return nil, fmt.Errorf("apply header style: %w", err)
	}

	rowNum := 2
	for _, v := range variants {
		productTitle := ""
		if v.Product != nil {
			productTitle = v.Product.Title
		}
		weightLb := math.Round(units.KgToLb(v.WeightKg)*100) / 100
		values := []any{
			v.ShopifyVariantID,
			productTitle,
			v.Title,
			v.SKU,
			fmt.Sprintf("%.2f", weightLb),
			fmt.Sprintf("%.2f", units.CmToIn(v.DepthCm)),
			fmt.Sprintf("%.2f", units.CmToIn(v.WidthCm)),
			fmt.Sprintf("%.2f", units.CmToIn(v.HeightCm)),
		}
		for col, val := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowNum)
			if err := f.SetCellValue(sheetName, cell, val); err != nil {
				return nil, fmt.Errorf("write row %d: %w", rowNum, err)
			}
		}
		rowNum++
	}

	if err := f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
		Selection: []excelize.Selection{
			{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"},
		},
	}); err != nil {
		return nil, fmt.Errorf("freeze header: %w", err)
	}

	cols := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	for _, col := range cols {
		maxLen := 0
		for row := 1; row < rowNum; row++ {
			val, _ := f.GetCellValue(sheetName, fmt.Sprintf("%s%d", col, row))
			if l := len([]rune(val)); l > maxLen {
				maxLen = l
			}
		}
		width := float64(maxLen) + 2
		if width < 12 {
			width = 12
		}
		if width > 60 {
			width = 60
		}
		if err := f.SetColWidth(sheetName, col, col, width); err != nil {
			return nil, fmt.Errorf("set column width: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("write excel: %w", err)
	}
	return buf.Bytes(), nil
}

func readPackagingImportRecords(filename string, r io.Reader) ([][]string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".xlsx":
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("invalid Excel file: %w", err)
		}
		defer func() { _ = f.Close() }()

		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, fmt.Errorf("Excel file has no sheets")
		}
		rows, err := f.GetRows(sheets[0])
		if err != nil {
			return nil, err
		}
		return rows, nil
	case ".csv", "":
		return csv.NewReader(r).ReadAll()
	default:
		return nil, fmt.Errorf("unsupported file type %q; use .xlsx or .csv", ext)
	}
}
