package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

// fakeOrderEditor does the arithmetic Shopify does in an edit session, so the service's
// "bill exactly what the app expects" check is exercised for real.
type fakeOrderEditor struct {
	before *shopify.OrderBalanceState
	after  *shopify.OrderBalanceState
	calc   *shopify.CalculatedOrder

	catalog    map[string]float64 // variant -> Shopify price
	inventory  map[string]shopify.InventoryAtLocation
	primary    *shopify.LocationRef
	primaryErr error

	discountPerLine   bool    // simulate Shopify applying a fixed discount to the whole line
	shippingSurcharge float64 // simulate Shopify billing more shipping than asked
	commitErr         error
	commitApplies     bool // with commitErr: the edit landed even though the call failed

	outstanding float64
	lineQty     map[string]int
	linePrice   map[string]float64
	shipping    map[string]float64
	added       int

	begun, commits, invoices, moves int
	notified                        bool
	applied                         bool
	addLocation, stockLocation      string
}

func (e *fakeOrderEditor) GetOrderBalanceState(context.Context, string) (*shopify.OrderBalanceState, error) {
	if e.applied && e.after != nil {
		return e.after, nil
	}
	return e.before, nil
}

func (e *fakeOrderEditor) BeginOrderEdit(context.Context, string) (*shopify.CalculatedOrder, error) {
	e.begun++
	e.outstanding = e.calc.Outstanding
	e.lineQty, e.linePrice, e.shipping = map[string]int{}, map[string]float64{}, map[string]float64{}
	for _, l := range e.calc.LineItems {
		e.lineQty[l.ID] = l.Quantity
		e.linePrice[l.ID] = l.DiscountedUnitPrice
	}
	for _, s := range e.calc.ShippingLines {
		e.shipping[s.ID] = s.Price
	}
	copied := *e.calc
	return &copied, nil
}

func (e *fakeOrderEditor) totals() *shopify.CalculatedOrderTotals {
	return &shopify.CalculatedOrderTotals{Outstanding: round2(e.outstanding)}
}

func (e *fakeOrderEditor) SetOrderEditQuantity(_ context.Context, _ string, lineID string, qty int) (*shopify.CalculatedOrderTotals, error) {
	e.outstanding -= float64(e.lineQty[lineID]-qty) * e.linePrice[lineID]
	e.lineQty[lineID] = qty
	return e.totals(), nil
}

func (e *fakeOrderEditor) AddOrderEditVariant(_ context.Context, _ string, variantID, locationID string, qty int) (*shopify.CalculatedLineItem, *shopify.CalculatedOrderTotals, error) {
	e.addLocation = locationID
	price, ok := e.catalog[variantID]
	if !ok {
		return nil, nil, fmt.Errorf("unknown variant %s", variantID)
	}
	e.added++
	id := fmt.Sprintf("gid://shopify/CalculatedLineItem/new-%d", e.added)
	e.lineQty[id], e.linePrice[id] = qty, price
	e.outstanding += price * float64(qty)
	return &shopify.CalculatedLineItem{ID: id, VariantID: variantID, Quantity: qty, OriginalUnitPrice: price, DiscountedUnitPrice: price}, e.totals(), nil
}

func (e *fakeOrderEditor) AddOrderEditLineDiscount(_ context.Context, _ string, lineID, _ string, amount float64) (*shopify.CalculatedLineItem, *shopify.CalculatedOrderTotals, error) {
	qty := e.lineQty[lineID]
	if e.discountPerLine {
		e.outstanding -= amount
		e.linePrice[lineID] -= amount / float64(qty)
	} else {
		e.outstanding -= amount * float64(qty)
		e.linePrice[lineID] -= amount
	}
	return &shopify.CalculatedLineItem{ID: lineID, Quantity: qty, DiscountedUnitPrice: round2(e.linePrice[lineID])}, e.totals(), nil
}

func (e *fakeOrderEditor) AddOrderEditShippingLine(_ context.Context, _ string, title string, amount float64) (*shopify.CalculatedShippingLine, *shopify.CalculatedOrderTotals, error) {
	id := "gid://shopify/CalculatedShippingLine/new"
	e.shipping[id] = amount
	e.outstanding += amount + e.shippingSurcharge
	return &shopify.CalculatedShippingLine{ID: id, Title: title, Price: amount}, e.totals(), nil
}

func (e *fakeOrderEditor) RemoveOrderEditShippingLine(_ context.Context, _ string, id string) (*shopify.CalculatedOrderTotals, error) {
	e.outstanding -= e.shipping[id]
	delete(e.shipping, id)
	return e.totals(), nil
}

func (e *fakeOrderEditor) CommitOrderEdit(_ context.Context, _ string, notify bool, _ string) error {
	e.commits++
	e.notified = notify
	e.applied = e.commitErr == nil || e.commitApplies
	return e.commitErr
}

func (e *fakeOrderEditor) GetPrimaryLocation(context.Context) (*shopify.LocationRef, error) {
	return e.primary, e.primaryErr
}

func (e *fakeOrderEditor) GetInventoryAtLocation(_ context.Context, _ []string, locationID string) (map[string]shopify.InventoryAtLocation, error) {
	e.stockLocation = locationID
	return e.inventory, nil
}

func (e *fakeOrderEditor) MoveFulfillmentOrderLines(context.Context, string, string, []shopify.FulfillmentOrderLineQuantity) error {
	e.moves++
	return nil
}

func (e *fakeOrderEditor) SendOrderInvoice(context.Context, string) error {
	e.invoices++
	return nil
}

// balanceEditStore records what the order-edit path saves.
type balanceEditStore struct {
	*updateShippingWarehouseStore
	others    []PreorderShipment
	stock     map[string]VariantStockRef
	completed *OrderEditBalanceResult
	released  int
	repriced  *struct {
		expected float64
		lineID   string
	}
	statuses map[string]string
}

func (s *balanceEditStore) GetPreorderShipments(context.Context, string) ([]PreorderShipment, error) {
	return append([]PreorderShipment{*s.shipment}, s.others...), nil
}

func (s *balanceEditStore) GetVariantStockRefs(context.Context, []string) (map[string]VariantStockRef, error) {
	return s.stock, nil
}

func (s *balanceEditStore) ClaimOrderEditBalance(_ context.Context, _ string, expected float64) (bool, error) {
	if s.shipment.BalanceMethod != nil || s.shipment.InvoiceSentAt != nil {
		return false, nil
	}
	method := BalanceMethodOrderEdit
	s.shipment.BalanceMethod = &method
	s.shipment.BalanceExpectedOutstanding = &expected
	s.shipment.UpdatedAt = time.Now()
	return true, nil
}

func (s *balanceEditStore) ReleaseOrderEditBalanceClaim(context.Context, string) error {
	s.released++
	s.shipment.BalanceMethod = nil
	s.shipment.BalanceExpectedOutstanding = nil
	return nil
}

func (s *balanceEditStore) CompleteOrderEditBalance(_ context.Context, _ string, result OrderEditBalanceResult) error {
	s.completed = &result
	method := BalanceMethodOrderEdit
	s.shipment.BalanceMethod = &method
	s.shipment.InvoiceSentAt = &result.SentAt
	s.shipment.InvoiceURL = &result.PaymentURL
	s.shipment.PrepaidApplied = result.PrepaidApplied
	s.shipment.BalanceExpectedOutstanding = &result.ExpectedOutstanding
	s.shipment.ShopifyShippingLineID = &result.ShippingLineID
	return nil
}

func (s *balanceEditStore) UpdateOrderEditBalanceShipping(_ context.Context, _ string, expected float64, lineID, _ string) error {
	s.repriced = &struct {
		expected float64
		lineID   string
	}{expected, lineID}
	return nil
}

func (s *balanceEditStore) UpdateItemStatusByID(_ context.Context, itemID, status string) error {
	if s.statuses == nil {
		s.statuses = map[string]string{}
	}
	s.statuses[itemID] = status
	return nil
}

const (
	testDefaultLocation = "gid://shopify/Location/91696955647"
	testDepositLine     = "gid://shopify/LineItem/11"
	testCalcDeposit     = "gid://shopify/CalculatedLineItem/11"
	testNewProductLine  = "gid://shopify/LineItem/21"
	testShippingTitle   = "Shipping & delivery - Remaining 50%"
)

// balanceEditFixture is the PRD example: 3 x After the Rain Shelf, deposit $175 each,
// website price $350, Shopify price $389, prepaid shipping $83.44, final shipping $170.
func balanceEditFixture() (*balanceEditStore, *fakeOrderEditor, *service) {
	unit, balance := 350.0, 525.0
	title := "After the Rain Shelf (Deposit 50%)"
	shopifyOrderID, depositLine := "1325", testDepositLine
	final := 170.0

	base := &updateShippingWarehouseStore{
		order: &Order{
			ID:              "order-1",
			OrderNumber:     "ORD-1325",
			ShopifyOrderID:  &shopifyOrderID,
			Customer:        &customer.Customer{Email: "buyer@example.com"},
			ShippingAddress: &customer.Address{Address1: "90 Dayton Ave", City: "Passaic", Province: "NJ", Zip: "07055", Country: "US"},
			Items: []OrderItem{{
				ID: "line-1", Type: "pre_order", ShopifyVariantID: shelfVariant, Quantity: 3, FulfillmentStep: 2,
				BalanceDue: &balance, Title: &title, UnitPrice: &unit, ShopifyLineItemID: &depositLine,
			}},
		},
		shipment: &PreorderShipment{
			ID: "ship-1", OrderID: "order-1", FinalShippingPrice: &final, PrepaidShipping: 83.44,
			WarehouseOrigin: "west", UpdatedAt: time.Now().Add(-time.Hour),
		},
	}
	store := &balanceEditStore{
		updateShippingWarehouseStore: base,
		stock: map[string]VariantStockRef{
			shelfVariant: {ShopifyVariantID: shelfVariant, InventoryItemID: "gid://shopify/InventoryItem/901", Tracked: true},
		},
	}
	editor := &fakeOrderEditor{
		before: &shopify.OrderBalanceState{
			Outstanding: 0,
			LineItems:   []shopify.OrderLineState{{ID: testDepositLine, Quantity: 3, CurrentQuantity: 3}},
		},
		after: &shopify.OrderBalanceState{
			Outstanding:          611.56,
			PaymentCollectionURL: "https://pay.example/1325",
			LineItems: []shopify.OrderLineState{
				{ID: testDepositLine, Quantity: 3, CurrentQuantity: 0},
				{ID: testNewProductLine, VariantID: shelfVariant, Quantity: 3, CurrentQuantity: 3},
			},
			ShippingLines: []shopify.OrderShippingLineState{{ID: "gid://shopify/ShippingLine/31", Title: testShippingTitle, Price: 86.56}},
		},
		calc: &shopify.CalculatedOrder{
			ID:        "gid://shopify/CalculatedOrder/1",
			LineItems: []shopify.CalculatedLineItem{depositCalcLine(testCalcDeposit, shelfVariant, 3, 175)},
		},
		catalog:   map[string]float64{shelfVariant: 389},
		inventory: map[string]shopify.InventoryAtLocation{"gid://shopify/InventoryItem/901": {Tracked: true, Available: 3}},
		primary:   &shopify.LocationRef{ID: testDefaultLocation, Name: "Manymoons"},
	}
	svc := &service{
		store:           store,
		preorderService: &capturingInvoicer{},
		shopClient:      &shippingDepositShop{},
		orderEditor:     editor,
		orderEditCfg:    OrderEditBalanceConfig{Enabled: true},
	}
	return store, editor, svc
}

// East or west only decides the shipping rate and who ships; the stock is checked and taken
// at Shopify's default location either way.
func TestRequestSecondPaymentByOrderEdit_TakesStockAtTheShopifyDefaultLocation(t *testing.T) {
	for _, origin := range []string{warehouse.CodeWest, warehouse.CodeEast} {
		t.Run(origin, func(t *testing.T) {
			store, editor, svc := balanceEditFixture()
			store.shipment.WarehouseOrigin = origin

			if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if editor.stockLocation != testDefaultLocation || editor.addLocation != testDefaultLocation {
				t.Fatalf("expected the default location for stock and lines, got %s / %s", editor.stockLocation, editor.addLocation)
			}
		})
	}
}

func TestRequestSecondPaymentByOrderEdit_StockMessageNamesTheDefaultLocation(t *testing.T) {
	_, editor, svc := balanceEditFixture()
	editor.inventory["gid://shopify/InventoryItem/901"] = shopify.InventoryAtLocation{Tracked: true, Available: 0}

	_, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
	if errCode(err) != "batch_stock_not_recorded" || !strings.Contains(err.Error(), "Manymoons") {
		t.Fatalf("expected a stock error naming Manymoons, got %v", err)
	}
}

func TestRequestSecondPaymentByOrderEdit_DefaultLocationLookupFails(t *testing.T) {
	_, editor, svc := balanceEditFixture()
	editor.primaryErr = errors.New("shopify down")

	_, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
	if errCode(err) != "shopify_order_edit_error" {
		t.Fatalf("expected shopify_order_edit_error, got %v", err)
	}
	if editor.begun != 0 {
		t.Fatal("nothing may be edited without a location")
	}
}

func TestRequestSecondPaymentByOrderEdit_BillsTheBalanceOnTheDepositOrder(t *testing.T) {
	store, editor, svc := balanceEditFixture()

	resp, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.InvoiceURL != "https://pay.example/1325" || resp.DraftOrderID != "" {
		t.Fatalf("expected the order's payment link and no draft, got %+v", resp)
	}
	if editor.commits != 1 || editor.notified {
		t.Fatalf("expected one commit without Shopify's email, got %d (notify=%v)", editor.commits, editor.notified)
	}
	if svc.preorderService.(*capturingInvoicer).captured.OrderID != "" {
		t.Fatal("no draft-order invoice may be created")
	}
	got := store.completed
	if got == nil {
		t.Fatal("expected the edit to be saved")
	}
	if got.ExpectedOutstanding != 611.56 || got.PrepaidApplied != 83.44 {
		t.Fatalf("expected outstanding 611.56 and prepaid 83.44, got %+v", got)
	}
	if got.ShippingLineID != "gid://shopify/ShippingLine/31" {
		t.Fatalf("expected the new shipping line, got %q", got.ShippingLineID)
	}
	if len(got.Lines) != 1 || got.Lines[0].ShopifyLineItemID != testNewProductLine ||
		got.Lines[0].OrderLineItemID != "line-1" || got.Lines[0].Quantity != 3 || got.Lines[0].UnitPrice != 350 {
		t.Fatalf("expected line-1 linked to the new product line, got %+v", got.Lines)
	}
	if store.statuses["line-1"] != "waiting_payment" || store.order.Items[0].FulfillmentStep != preOrderStepSecondPayment {
		t.Fatalf("expected the item waiting for payment at step 3, got %q / %d", store.statuses["line-1"], store.order.Items[0].FulfillmentStep)
	}

	// A second click returns the same link and does not edit again.
	again, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
	if err != nil || again.InvoiceURL != resp.InvoiceURL || editor.commits != 1 {
		t.Fatalf("expected the existing link without a new edit, got %+v err=%v commits=%d", again, err, editor.commits)
	}
}

func TestRequestSecondPaymentByOrderEdit_RefusesBeforeTouchingShopify(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*balanceEditStore, *fakeOrderEditor)
		code  string
	}{
		{
			name: "batch stock not recorded",
			setup: func(_ *balanceEditStore, e *fakeOrderEditor) {
				e.inventory["gid://shopify/InventoryItem/901"] = shopify.InventoryAtLocation{Tracked: true, Available: 1}
			},
			code: "batch_stock_not_recorded",
		},
		{
			name: "another group awaits payment",
			setup: func(s *balanceEditStore, _ *fakeOrderEditor) {
				method, sent := BalanceMethodOrderEdit, time.Now()
				s.others = []PreorderShipment{{ID: "ship-2", OrderID: "order-1", BalanceMethod: &method, InvoiceSentAt: &sent}}
			},
			code: "balance_pending_other_group",
		},
		{
			name: "the Shopify order already owes money",
			setup: func(_ *balanceEditStore, e *fakeOrderEditor) {
				e.before.Outstanding = 12.50
			},
			code: "balance_pending_other_group",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, editor, svc := balanceEditFixture()
			tc.setup(store, editor)

			_, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
			if got := errCode(err); got != tc.code {
				t.Fatalf("expected %s, got %q (%v)", tc.code, got, err)
			}
			if editor.begun != 0 || editor.commits != 0 || store.completed != nil {
				t.Fatalf("nothing may be edited: begun=%d commits=%d", editor.begun, editor.commits)
			}
		})
	}
}

func TestRequestSecondPaymentByOrderEdit_DiscardsAnEditThatWouldBillWrong(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeOrderEditor)
		code  string
	}{
		{"website price above Shopify price", func(e *fakeOrderEditor) { e.catalog[shelfVariant] = 300 }, "ws_price_above_catalog"},
		{"discount applied to the whole line", func(e *fakeOrderEditor) { e.discountPerLine = true }, "balance_mismatch"},
		{"Shopify total differs from the app", func(e *fakeOrderEditor) { e.shippingSurcharge = 1 }, "balance_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, editor, svc := balanceEditFixture()
			tc.setup(editor)

			_, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
			if got := errCode(err); got != tc.code {
				t.Fatalf("expected %s, got %q (%v)", tc.code, got, err)
			}
			if editor.commits != 0 || store.completed != nil || store.shipment.BalanceMethod != nil {
				t.Fatalf("the edit must be discarded unclaimed: commits=%d", editor.commits)
			}
		})
	}
}

// When Shopify applied the edit but the call failed, the next request finishes it instead
// of billing the customer twice.
func TestRequestSecondPaymentByOrderEdit_AdoptsAnEditWhoseCommitReplyWasLost(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	editor.commitErr, editor.commitApplies = errors.New("timeout"), true

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); errCode(err) != "shopify_order_edit_error" {
		t.Fatalf("expected the commit failure to surface, got %v", err)
	}
	if !store.shipment.IsOrderEditBalance() || store.shipment.InvoiceSentAt != nil {
		t.Fatal("the claim must stay so a retry can finish the edit")
	}

	// An immediate retry is told to wait; after the claim timeout it adopts.
	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); errCode(err) != "balance_edit_in_progress" {
		t.Fatalf("expected in-progress, got %v", err)
	}
	store.shipment.UpdatedAt = time.Now().Add(-time.Hour)
	editor.commitErr = nil

	resp, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.commits != 1 {
		t.Fatalf("the edit must not be committed twice, got %d commits", editor.commits)
	}
	if resp.InvoiceURL != "https://pay.example/1325" || store.completed == nil || len(store.completed.Lines) != 1 {
		t.Fatalf("expected the earlier edit adopted, got %+v / %+v", resp, store.completed)
	}
}

// When the edit never landed, the retry releases the claim and edits again.
func TestRequestSecondPaymentByOrderEdit_RedoesAnEditThatNeverLanded(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	editor.commitErr = errors.New("shopify down")

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err == nil {
		t.Fatal("expected the first request to fail")
	}
	store.shipment.UpdatedAt = time.Now().Add(-time.Hour)
	editor.commitErr = nil

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.released != 1 || editor.commits != 2 || store.completed == nil {
		t.Fatalf("expected release + a fresh edit, got released=%d commits=%d", store.released, editor.commits)
	}
}

func TestRequestSecondPayment_DraftInvoiceWhenOrderEditIsOff(t *testing.T) {
	_, editor, svc := balanceEditFixture()
	svc.orderEditCfg.Enabled = false

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.begun != 0 {
		t.Fatal("the order must not be edited when the switch is off")
	}
	if svc.preorderService.(*capturingInvoicer).captured.OrderID != "order-1" {
		t.Fatal("expected a draft-order invoice")
	}
}

// Re-pricing replaces only the group's shipping line and keeps the link.
func TestUpdatePreorderShipping_RepricesAnOrderEditBalance(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	method, sent, url, lineID := BalanceMethodOrderEdit, time.Now(), "https://pay.example/1325", "gid://shopify/ShippingLine/31"
	expected := 611.56
	store.shipment.BalanceMethod = &method
	store.shipment.InvoiceSentAt = &sent
	store.shipment.InvoiceURL = &url
	store.shipment.ShopifyShippingLineID = &lineID
	store.shipment.BalanceExpectedOutstanding = &expected
	store.shipment.PrepaidApplied = 83.44
	editor.before = &shopify.OrderBalanceState{
		Outstanding:   611.56,
		ShippingLines: []shopify.OrderShippingLineState{{ID: lineID, Title: testShippingTitle, Price: 86.56}},
	}
	editor.calc = &shopify.CalculatedOrder{
		ID:            "gid://shopify/CalculatedOrder/2",
		Outstanding:   611.56,
		ShippingLines: []shopify.CalculatedShippingLine{{ID: "gid://shopify/CalculatedShippingLine/31", Title: testShippingTitle, Price: 86.56}},
	}
	editor.after = &shopify.OrderBalanceState{
		Outstanding:          621.56,
		PaymentCollectionURL: url,
		ShippingLines:        []shopify.OrderShippingLineState{{ID: "gid://shopify/ShippingLine/32", Title: testShippingTitle, Price: 96.56}},
	}

	if _, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{FinalShippingPrice: 180}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.commits != 1 {
		t.Fatalf("expected one re-price edit, got %d", editor.commits)
	}
	// 611.56 - 86.56 + (180 - 83.44)
	if store.repriced == nil || store.repriced.expected != 621.56 || store.repriced.lineID != "gid://shopify/ShippingLine/32" {
		t.Fatalf("expected outstanding 621.56 on shipping line 32, got %+v", store.repriced)
	}
	if store.shipment.FinalShippingPrice == nil || *store.shipment.FinalShippingPrice != 180 {
		t.Fatalf("expected the new final price saved, got %v", store.shipment.FinalShippingPrice)
	}
}

// While an edit's outcome is unknown the shipping it was priced with must not change.
func TestUpdatePreorderShipping_RefusedWhileABalanceEditIsUnfinished(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	method := BalanceMethodOrderEdit
	store.shipment.BalanceMethod = &method // claimed, never saved

	_, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{FinalShippingPrice: 180})
	if errCode(err) != "balance_edit_in_progress" {
		t.Fatalf("expected balance_edit_in_progress, got %v", err)
	}
	if editor.begun != 0 || *store.shipment.FinalShippingPrice != 170 {
		t.Fatal("nothing may change while the edit is unfinished")
	}
}

func TestUpdatePreorderShipping_OrderEditBalanceAlreadyPaidInShopify(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	method, sent, url := BalanceMethodOrderEdit, time.Now(), "https://pay.example/1325"
	store.shipment.BalanceMethod = &method
	store.shipment.InvoiceSentAt = &sent
	store.shipment.InvoiceURL = &url
	editor.before = &shopify.OrderBalanceState{Outstanding: 0}

	_, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{FinalShippingPrice: 180})
	if !errors.Is(err, errSecondPaymentAlreadyPaid) {
		t.Fatalf("expected already-paid, got %v", err)
	}
	if editor.begun != 0 {
		t.Fatal("a paid order must not be edited")
	}
}
