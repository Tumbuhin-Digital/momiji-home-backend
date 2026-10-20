package order

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func salesReportOrders() []Order {
	return []Order{
		{
			OrderNumber: "ORD-1", CreatedAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
			TotalPrice: 100.10, FinancialStatus: "paid", FulfillmentStatus: "pending",
			Items: []OrderItem{{Type: "ship_ready", Quantity: 2}, {Type: "pre_order", Quantity: 1}},
		},
		{
			OrderNumber: "ORD-2", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			TotalPrice: 50.25, FinancialStatus: "paid", FulfillmentStatus: "pending",
			Items: []OrderItem{{Type: "pre_order", Quantity: 3}},
		},
		{
			OrderNumber: "ORD-3", CreatedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC),
			TotalPrice: 20, FinancialStatus: "pending", FulfillmentStatus: "fulfilled",
			Items: []OrderItem{{Type: "ship_ready", Quantity: 1}},
		},
	}
}

func TestSummarizeSales(t *testing.T) {
	sum := summarizeSales(salesReportOrders())

	if sum.Orders != 3 || sum.Revenue != 170.35 || sum.ShipReadyItems != 3 || sum.PreOrderItems != 4 {
		t.Fatalf("unexpected totals: %+v", sum)
	}
	wantMonths := []salesBucket{{"Sep 2026", 1, 100.10}, {"Oct 2026", 2, 70.25}}
	if len(sum.ByMonth) != len(wantMonths) {
		t.Fatalf("months = %+v", sum.ByMonth)
	}
	for i, want := range wantMonths {
		if sum.ByMonth[i] != want {
			t.Fatalf("month %d = %+v, want %+v", i, sum.ByMonth[i], want)
		}
	}
	if got := sum.ByFulfillment[0]; got.Label != "pending" || got.Orders != 2 {
		t.Fatalf("expected pending first, got %+v", sum.ByFulfillment)
	}
	if got := sum.ByFinancial[0]; got.Label != "paid" || got.Orders != 2 || got.Revenue != 150.35 {
		t.Fatalf("expected paid first, got %+v", sum.ByFinancial)
	}
}

func TestBuildSalesReport(t *testing.T) {
	data, err := buildSalesReport(salesReportOrders(), OrderQuery{Status: "paid"}, time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	if got := f.GetSheetList(); len(got) != 2 || got[0] != salesSheet || got[1] != summarySheet {
		t.Fatalf("sheets = %v", got)
	}

	// Total Price is a real number shown as currency, not text.
	raw, _ := f.GetCellValue(salesSheet, "G2", excelize.Options{RawCellValue: true})
	shown, _ := f.GetCellValue(salesSheet, "G2")
	if raw != "100.1" || shown != "$100.10" {
		t.Fatalf("G2 raw=%q shown=%q", raw, shown)
	}

	if label, _ := f.GetCellValue(salesSheet, "A5"); label != "Total" {
		t.Fatalf("A5 = %q, want Total", label)
	}
	for cell, want := range map[string]string{
		"E5": "SUBTOTAL(109,E2:E4)",
		"F5": "SUBTOTAL(109,F2:F4)",
		"G5": "SUBTOTAL(109,G2:G4)",
	} {
		if got, _ := f.GetCellFormula(salesSheet, cell); got != want {
			t.Fatalf("%s formula = %q, want %q", cell, got, want)
		}
	}

	if note, _ := f.GetCellValue(summarySheet, "A2"); note != "Generated 2026-10-19 08:00 · Status: paid" {
		t.Fatalf("note = %q", note)
	}
	if orders, _ := f.GetCellValue(summarySheet, "B5"); orders != "3" {
		t.Fatalf("total orders = %q", orders)
	}
	if revenue, _ := f.GetCellValue(summarySheet, "B6"); revenue != "$170.35" {
		t.Fatalf("total revenue = %q", revenue)
	}

	if n := countCharts(t, data); n != 2 {
		t.Fatalf("expected 2 charts, got %d", n)
	}
}

func TestBuildSalesReport_NoOrders(t *testing.T) {
	data, err := buildSalesReport(nil, OrderQuery{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.GetCellValue(salesSheet, "A2"); v != "" {
		t.Fatalf("expected no totals row, got A2=%q", v)
	}
	if orders, _ := f.GetCellValue(summarySheet, "B5"); orders != "0" {
		t.Fatalf("total orders = %q", orders)
	}
	if n := countCharts(t, data); n != 0 {
		t.Fatalf("expected no charts, got %d", n)
	}
}

func countCharts(t *testing.T, data []byte) int {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, file := range zr.File {
		if strings.HasPrefix(file.Name, "xl/charts/chart") {
			n++
		}
	}
	return n
}
