package order

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	salesSheet   = "Sales Report"
	summarySheet = "Summary"

	moneyNumFmt = `$#,##0.00`
	// chartColor is the single-series bar color, chosen for contrast on a white sheet.
	chartColor = "2A78D6"
	// chartRows is how many default-height rows a summary chart covers.
	chartRows = 16
)

var salesHeaders = []string{"Order ID", "Date", "Customer Name", "Customer Email", "Ship Ready Items", "Pre-Order Items", "Total Price", "Financial Status", "Fulfillment Status"}

// salesBucket aggregates the orders that share a month or a status.
type salesBucket struct {
	Label   string
	Orders  int
	Revenue float64
}

type salesSummary struct {
	Orders         int
	Revenue        float64
	ShipReadyItems int
	PreOrderItems  int
	ByMonth        []salesBucket // oldest month first
	ByFulfillment  []salesBucket // most orders first
	ByFinancial    []salesBucket // most orders first
}

type salesReportStyles struct {
	title, note, section, header, money, total, totalMoney int
}

// buildSalesReport writes the exported orders plus a totals row, and a Summary sheet
// with headline figures, monthly and per-status breakdowns, and charts.
func buildSalesReport(orders []Order, query OrderQuery, generatedAt time.Time) ([]byte, error) {
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", salesSheet)

	styles, err := newSalesReportStyles(f)
	if err != nil {
		return nil, err
	}
	if err := writeSalesSheet(f, orders, styles); err != nil {
		return nil, err
	}
	if err := writeSummarySheet(f, summarizeSales(orders), query, generatedAt, styles); err != nil {
		return nil, err
	}

	// The totals row is formulas; make Excel calculate them when the file opens.
	fullCalc := true
	if err := f.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &fullCalc}); err != nil {
		return nil, fmt.Errorf("failed to set calc props: %w", err)
	}
	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write excel file: %w", err)
	}
	return buf.Bytes(), nil
}

func newSalesReportStyles(f *excelize.File) (salesReportStyles, error) {
	moneyFmt := moneyNumFmt
	topBorder := []excelize.Border{{Type: "top", Color: "000000", Style: 1}}
	defs := []*excelize.Style{
		{Font: &excelize.Font{Bold: true, Size: 14}},
		{Font: &excelize.Font{Color: "666666"}},
		{Font: &excelize.Font{Bold: true, Size: 12}},
		{Font: &excelize.Font{Bold: true}},
		{CustomNumFmt: &moneyFmt},
		{Font: &excelize.Font{Bold: true}, Border: topBorder},
		{Font: &excelize.Font{Bold: true}, Border: topBorder, CustomNumFmt: &moneyFmt},
	}
	ids := make([]int, len(defs))
	for i, def := range defs {
		id, err := f.NewStyle(def)
		if err != nil {
			return salesReportStyles{}, fmt.Errorf("failed to create style: %w", err)
		}
		ids[i] = id
	}
	return salesReportStyles{
		title: ids[0], note: ids[1], section: ids[2], header: ids[3],
		money: ids[4], total: ids[5], totalMoney: ids[6],
	}, nil
}

func orderItemCounts(o Order) (shipReady, preOrder int) {
	for _, item := range o.Items {
		if item.Type == "ship_ready" {
			shipReady += item.Quantity
		} else if item.Type == "pre_order" {
			preOrder += item.Quantity
		}
	}
	return shipReady, preOrder
}

func writeSalesSheet(f *excelize.File, orders []Order, st salesReportStyles) error {
	for i, h := range salesHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(salesSheet, cell, h)
	}

	for rowIdx, o := range orders {
		shipReadyQty, preOrderQty := orderItemCounts(o)

		rowNum := rowIdx + 2
		f.SetCellValue(salesSheet, fmt.Sprintf("A%d", rowNum), o.OrderNumber)
		f.SetCellValue(salesSheet, fmt.Sprintf("B%d", rowNum), o.CreatedAt.Format("2006-01-02"))
		if o.Customer != nil {
			var fname, lname string
			if o.Customer.FirstName != nil {
				fname = *o.Customer.FirstName
			}
			if o.Customer.LastName != nil {
				lname = *o.Customer.LastName
			}
			f.SetCellValue(salesSheet, fmt.Sprintf("C%d", rowNum), fname+" "+lname)
			f.SetCellValue(salesSheet, fmt.Sprintf("D%d", rowNum), o.Customer.Email)
		}
		f.SetCellValue(salesSheet, fmt.Sprintf("E%d", rowNum), shipReadyQty)
		f.SetCellValue(salesSheet, fmt.Sprintf("F%d", rowNum), preOrderQty)
		f.SetCellValue(salesSheet, fmt.Sprintf("G%d", rowNum), round2(o.TotalPrice))
		f.SetCellValue(salesSheet, fmt.Sprintf("H%d", rowNum), o.FinancialStatus)
		f.SetCellValue(salesSheet, fmt.Sprintf("I%d", rowNum), o.FulfillmentStatus)
	}

	lastRow := len(orders) + 1
	if err := f.SetCellStyle(salesSheet, "A1", "I1", st.header); err != nil {
		return fmt.Errorf("failed to apply header style: %w", err)
	}
	if err := f.SetPanes(salesSheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
		Selection: []excelize.Selection{
			{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"},
		},
	}); err != nil {
		return fmt.Errorf("failed to freeze header row: %w", err)
	}

	if len(orders) > 0 {
		if err := f.SetCellStyle(salesSheet, "G2", fmt.Sprintf("G%d", lastRow), st.money); err != nil {
			return fmt.Errorf("failed to apply money style: %w", err)
		}
		if err := f.AutoFilter(salesSheet, fmt.Sprintf("A1:I%d", lastRow), nil); err != nil {
			return fmt.Errorf("failed to add filter: %w", err)
		}
		if err := writeSalesTotals(f, orders, lastRow, st); err != nil {
			return err
		}
		lastRow++
	}

	// Size each column to its longest value so Excel does not cut text off.
	for i := range salesHeaders {
		col, _ := excelize.ColumnNumberToName(i + 1)
		maxLen := 0
		for row := 1; row <= lastRow; row++ {
			val, _ := f.GetCellValue(salesSheet, fmt.Sprintf("%s%d", col, row))
			if l := len([]rune(val)); l > maxLen {
				maxLen = l
			}
		}
		width := float64(maxLen) + 2
		if width < 10 {
			width = 10
		}
		if width > 55 {
			width = 55
		}
		if err := f.SetColWidth(salesSheet, col, col, width); err != nil {
			return fmt.Errorf("failed to set column width: %w", err)
		}
	}
	return nil
}

// writeSalesTotals adds a totals row under the data. SUBTOTAL(109, …) sums only the
// rows left visible by the filter, so the totals follow whatever the admin filters on.
func writeSalesTotals(f *excelize.File, orders []Order, lastRow int, st salesReportStyles) error {
	sum := summarizeSales(orders)
	row := lastRow + 1
	f.SetCellValue(salesSheet, fmt.Sprintf("A%d", row), "Total")
	for _, c := range []struct {
		col    string
		cached any
	}{
		{"E", sum.ShipReadyItems},
		{"F", sum.PreOrderItems},
		{"G", round2(sum.Revenue)},
	} {
		cell := fmt.Sprintf("%s%d", c.col, row)
		// The cached value shows in viewers that do not recalculate formulas.
		f.SetCellValue(salesSheet, cell, c.cached)
		if err := f.SetCellFormula(salesSheet, cell, fmt.Sprintf("SUBTOTAL(109,%s2:%s%d)", c.col, c.col, lastRow)); err != nil {
			return fmt.Errorf("failed to set total formula: %w", err)
		}
	}
	if err := f.SetCellStyle(salesSheet, fmt.Sprintf("A%d", row), fmt.Sprintf("I%d", row), st.total); err != nil {
		return fmt.Errorf("failed to apply total style: %w", err)
	}
	if err := f.SetCellStyle(salesSheet, fmt.Sprintf("G%d", row), fmt.Sprintf("G%d", row), st.totalMoney); err != nil {
		return fmt.Errorf("failed to apply total style: %w", err)
	}
	return nil
}

func summarizeSales(orders []Order) salesSummary {
	var sum salesSummary
	months := map[string]*salesBucket{}
	fulfillment := map[string]*salesBucket{}
	financial := map[string]*salesBucket{}
	for _, o := range orders {
		shipReady, preOrder := orderItemCounts(o)
		sum.Orders++
		sum.Revenue += o.TotalPrice
		sum.ShipReadyItems += shipReady
		sum.PreOrderItems += preOrder
		addToBucket(months, o.CreatedAt.Format("2006-01"), o.TotalPrice)
		addToBucket(fulfillment, statusLabel(o.FulfillmentStatus), o.TotalPrice)
		addToBucket(financial, statusLabel(o.FinancialStatus), o.TotalPrice)
	}

	sum.ByMonth = sortedBuckets(months, func(a, b salesBucket) bool { return a.Label < b.Label })
	for i := range sum.ByMonth {
		if t, err := time.Parse("2006-01", sum.ByMonth[i].Label); err == nil {
			sum.ByMonth[i].Label = t.Format("Jan 2006")
		}
	}
	byOrders := func(a, b salesBucket) bool {
		if a.Orders != b.Orders {
			return a.Orders > b.Orders
		}
		return a.Label < b.Label
	}
	sum.ByFulfillment = sortedBuckets(fulfillment, byOrders)
	sum.ByFinancial = sortedBuckets(financial, byOrders)
	return sum
}

func addToBucket(buckets map[string]*salesBucket, label string, revenue float64) {
	b, ok := buckets[label]
	if !ok {
		b = &salesBucket{Label: label}
		buckets[label] = b
	}
	b.Orders++
	b.Revenue += revenue
}

func sortedBuckets(buckets map[string]*salesBucket, less func(a, b salesBucket) bool) []salesBucket {
	out := make([]salesBucket, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, salesBucket{Label: b.Label, Orders: b.Orders, Revenue: round2(b.Revenue)})
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

func statusLabel(status string) string {
	if strings.TrimSpace(status) == "" {
		return "(none)"
	}
	return status
}

// bucketChart describes the single-series chart drawn beside a breakdown table.
type bucketChart struct {
	Type              excelize.ChartType
	Title             string
	ValueCol          string // "B" for orders, "C" for revenue
	NumFmt            string
	MajorUnit         float64 // 0 lets Excel pick the step
	ReverseCategories bool    // keep the first table row at the top of a horizontal bar chart
}

// countTickUnit picks a whole-number axis step so order counts never show
// fractional, repeated tick labels.
func countTickUnit(buckets []salesBucket) float64 {
	most := 0
	for _, b := range buckets {
		if b.Orders > most {
			most = b.Orders
		}
	}
	return math.Max(1, math.Ceil(float64(most)/5))
}

func writeSummarySheet(f *excelize.File, sum salesSummary, query OrderQuery, generatedAt time.Time, st salesReportStyles) error {
	if _, err := f.NewSheet(summarySheet); err != nil {
		return fmt.Errorf("failed to create summary sheet: %w", err)
	}

	f.SetCellValue(summarySheet, "A1", "Sales Summary")
	f.SetCellValue(summarySheet, "A2", summaryNote(query, generatedAt))
	f.SetCellValue(summarySheet, "A4", "Overview")
	if err := setSummaryStyle(f, "A1", "A1", st.title); err != nil {
		return err
	}
	if err := setSummaryStyle(f, "A2", "A2", st.note); err != nil {
		return err
	}
	if err := setSummaryStyle(f, "A4", "A4", st.section); err != nil {
		return err
	}

	avg := 0.0
	if sum.Orders > 0 {
		avg = sum.Revenue / float64(sum.Orders)
	}
	overview := []struct {
		label string
		value any
		money bool
	}{
		{"Total Orders", sum.Orders, false},
		{"Total Revenue", round2(sum.Revenue), true},
		{"Average Order Value", round2(avg), true},
		{"Ship Ready Items", sum.ShipReadyItems, false},
		{"Pre-Order Items", sum.PreOrderItems, false},
	}
	for i, item := range overview {
		row := 5 + i
		f.SetCellValue(summarySheet, fmt.Sprintf("A%d", row), item.label)
		f.SetCellValue(summarySheet, fmt.Sprintf("B%d", row), item.value)
		if item.money {
			cell := fmt.Sprintf("B%d", row)
			if err := setSummaryStyle(f, cell, cell, st.money); err != nil {
				return err
			}
		}
	}

	if sum.Orders > 0 {
		row := 5 + len(overview) + 1
		var err error
		row, err = writeBucketSection(f, row, "Sales by Month", "Month", sum.ByMonth, &bucketChart{
			Type: excelize.Col, Title: "Revenue by Month", ValueCol: "C", NumFmt: "$#,##0",
		}, st)
		if err != nil {
			return err
		}
		row, err = writeBucketSection(f, row, "Sales by Fulfillment Status", "Status", sum.ByFulfillment, &bucketChart{
			Type: excelize.Bar, Title: "Orders by Fulfillment Status", ValueCol: "B", NumFmt: "0",
			MajorUnit: countTickUnit(sum.ByFulfillment), ReverseCategories: true,
		}, st)
		if err != nil {
			return err
		}
		if _, err := writeBucketSection(f, row, "Sales by Financial Status", "Status", sum.ByFinancial, nil, st); err != nil {
			return err
		}
	}

	for col, width := range map[string]float64{"A": 26, "B": 14, "C": 16} {
		if err := f.SetColWidth(summarySheet, col, col, width); err != nil {
			return fmt.Errorf("failed to set column width: %w", err)
		}
	}
	return nil
}

// writeBucketSection writes a titled Label/Orders/Revenue table starting at row start,
// with an optional chart to its right, and returns the first free row after it.
func writeBucketSection(f *excelize.File, start int, title, labelHeader string, buckets []salesBucket, chart *bucketChart, st salesReportStyles) (int, error) {
	titleCell := fmt.Sprintf("A%d", start)
	f.SetCellValue(summarySheet, titleCell, title)
	if err := setSummaryStyle(f, titleCell, titleCell, st.section); err != nil {
		return 0, err
	}

	header := start + 1
	f.SetCellValue(summarySheet, fmt.Sprintf("A%d", header), labelHeader)
	f.SetCellValue(summarySheet, fmt.Sprintf("B%d", header), "Orders")
	f.SetCellValue(summarySheet, fmt.Sprintf("C%d", header), "Revenue")
	if err := setSummaryStyle(f, fmt.Sprintf("A%d", header), fmt.Sprintf("C%d", header), st.header); err != nil {
		return 0, err
	}

	first := header + 1
	for i, b := range buckets {
		row := first + i
		f.SetCellValue(summarySheet, fmt.Sprintf("A%d", row), b.Label)
		f.SetCellValue(summarySheet, fmt.Sprintf("B%d", row), b.Orders)
		f.SetCellValue(summarySheet, fmt.Sprintf("C%d", row), b.Revenue)
	}
	last := first + len(buckets) - 1
	if len(buckets) > 0 {
		if err := setSummaryStyle(f, fmt.Sprintf("C%d", first), fmt.Sprintf("C%d", last), st.money); err != nil {
			return 0, err
		}
	}

	next := last + 3
	if chart == nil || len(buckets) == 0 {
		return next, nil
	}

	// Bars must start at zero, and axis text needs to be darker than excelize's default.
	zero := 0.0
	axisFont := excelize.Font{Color: "595959"}
	if err := f.AddChart(summarySheet, fmt.Sprintf("E%d", start), &excelize.Chart{
		Type: chart.Type,
		Series: []excelize.ChartSeries{{
			Name:       fmt.Sprintf("%s!$%s$%d", summarySheet, chart.ValueCol, header),
			Categories: fmt.Sprintf("%s!$A$%d:$A$%d", summarySheet, first, last),
			Values:     fmt.Sprintf("%s!$%s$%d:$%s$%d", summarySheet, chart.ValueCol, first, chart.ValueCol, last),
			Fill:       excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{chartColor}},
		}},
		Title:     []excelize.RichTextRun{{Text: chart.Title}},
		Legend:    excelize.ChartLegend{Position: "none"},
		Dimension: excelize.ChartDimension{Width: 560, Height: 300},
		XAxis:     excelize.ChartAxis{ReverseOrder: chart.ReverseCategories, Font: axisFont},
		YAxis: excelize.ChartAxis{
			MajorGridLines: true,
			Minimum:        &zero,
			MajorUnit:      chart.MajorUnit,
			Font:           axisFont,
			NumFmt:         excelize.ChartNumFmt{CustomNumFmt: chart.NumFmt},
		},
	}); err != nil {
		return 0, fmt.Errorf("failed to add %s chart: %w", chart.Title, err)
	}
	if end := start + chartRows + 1; end > next {
		next = end
	}
	return next, nil
}

func setSummaryStyle(f *excelize.File, from, to string, style int) error {
	if err := f.SetCellStyle(summarySheet, from, to, style); err != nil {
		return fmt.Errorf("failed to apply summary style: %w", err)
	}
	return nil
}

// summaryNote says when the report was made and which filters narrowed it.
func summaryNote(query OrderQuery, generatedAt time.Time) string {
	parts := []string{"Generated " + generatedAt.Format("2006-01-02 15:04")}
	if s := strings.TrimSpace(query.Status); s != "" {
		parts = append(parts, "Status: "+s)
	}
	if s := strings.TrimSpace(query.Search); s != "" {
		parts = append(parts, "Search: "+s)
	}
	return strings.Join(parts, " · ")
}
