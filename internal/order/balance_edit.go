package order

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

// Pre-order balances billed by editing the deposit order, so one purchase stays one
// Shopify order and the balance shows the real products. See
// docs/prd/pre-order-revisi-satu-order.md.

// OrderEditBalanceConfig switches new pre-order balances from draft invoices to order edits.
// Stock for the added product lines is checked and taken at Shopify's default location.
type OrderEditBalanceConfig struct {
	Enabled bool
}

// balanceEditClaimTimeout is how long a claimed-but-unsaved edit is assumed to still be
// running before a retry may look at Shopify and finish or redo it.
const balanceEditClaimTimeout = 2 * time.Minute

const wholesaleDiscountDescription = "Wholesale price"

// SetOrderEditBalance enables billing balances by order edit. Groups already billed by
// draft invoice keep that path whatever the setting.
func (s *service) SetOrderEditBalance(editor shopify.OrderEditor, cfg OrderEditBalanceConfig) {
	s.orderEditor = editor
	s.orderEditCfg = cfg
}

// useOrderEditBalance decides how a group's balance is billed. A group already on the
// order-edit path stays on it so an interrupted edit is finished rather than duplicated.
func (s *service) useOrderEditBalance(sh *PreorderShipment) bool {
	if sh.IsOrderEditBalance() {
		return true
	}
	if sh.InvoiceSentAt != nil {
		return false
	}
	return s.orderEditCfg.Enabled && s.orderEditor != nil
}

func (s *service) requestSecondPaymentByOrderEdit(ctx context.Context, userID string, plan *groupSecondPaymentPlan) (*RequestSecondPaymentResponse, error) {
	o, sh := plan.order, plan.shipment
	if s.orderEditor == nil {
		return nil, errBalanceEdit("balance_edit_not_configured", "Billing the balance on the original order is not configured")
	}
	if o.ShopifyOrderID == nil || *o.ShopifyOrderID == "" {
		return nil, errBalanceEdit("balance_edit_unavailable", "This order has no Shopify order to bill the balance on")
	}
	shopifyOrderID := *o.ShopifyOrderID

	shipments, err := s.store.GetPreorderShipments(ctx, o.ID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	for i := range shipments {
		other := &shipments[i]
		if other.ID == sh.ID || !other.IsOrderEditBalance() || other.InvoicePaidAt != nil {
			continue
		}
		// Shopify keeps one outstanding balance per order, so a payment could not be told
		// apart between two groups billed at the same time.
		return nil, errBalanceEdit("balance_pending_other_group",
			"Another pre-order group on this order is still awaiting its balance payment. Request this group once that one is paid.")
	}

	location, err := s.balanceLocation(ctx)
	if err != nil {
		return nil, err
	}
	locationID := location.ID

	if sh.IsOrderEditBalance() && sh.InvoiceSentAt == nil {
		if time.Since(sh.UpdatedAt) < balanceEditClaimTimeout {
			return nil, errBalanceEdit("balance_edit_in_progress",
				"This group's balance is already being requested. Wait a minute and refresh.")
		}
		resp, adopted, err := s.adoptOrderEditBalance(ctx, userID, plan, locationID)
		if err != nil || adopted {
			return resp, err
		}
		if err := s.store.ReleaseOrderEditBalanceClaim(ctx, sh.ID); err != nil {
			return nil, apierror.ErrInternal
		}
	}

	lines, err := balanceEditLines(plan)
	if err != nil {
		return nil, err
	}
	if err := s.checkBatchStock(ctx, lines, location); err != nil {
		return nil, err
	}

	before, err := s.orderEditor.GetOrderBalanceState(ctx, shopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: load Shopify order failed", "order_id", o.ID, "error", err)
		return nil, errShopifyOrderEdit()
	}
	if before.Outstanding > moneyTolerance {
		return nil, errBalanceEdit("balance_pending_other_group",
			fmt.Sprintf("The Shopify order already has an unpaid balance of $%.2f. Collect or clear it before requesting this group's balance.", before.Outstanding))
	}

	calc, err := s.orderEditor.BeginOrderEdit(ctx, shopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: begin failed", "order_id", o.ID, "error", err)
		return nil, errShopifyOrderEdit()
	}
	shippingTitle := balanceShippingTitle(plan.opts)
	editPlan, err := planBalanceEdit(lines, calc.LineItems, plan.opts.ShippingPrice, shippingTitle)
	if err != nil {
		return nil, err
	}
	totals, err := s.applyBalanceEdit(ctx, calc.ID, editPlan, locationID)
	if err != nil {
		return nil, err
	}

	// The edit is only committed if Shopify would bill exactly what the app expects; any
	// difference leaves the order untouched (an uncommitted edit session is discarded).
	expected := round2(before.Outstanding + editPlan.OutstandingIncrease)
	if !moneyEqual(totals.Outstanding, expected) {
		slog.ErrorContext(ctx, "balance edit: Shopify outstanding differs from app balance; edit discarded",
			"order_id", o.ID, "shipment_id", sh.ID, "shopify_outstanding", totals.Outstanding, "expected", expected)
		return nil, errBalanceEdit("balance_mismatch",
			fmt.Sprintf("Shopify would bill $%.2f but the app expects $%.2f. Nothing was changed; contact the tech team.", totals.Outstanding, expected))
	}

	claimed, err := s.store.ClaimOrderEditBalance(ctx, sh.ID, expected)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if !claimed {
		return nil, errBalanceEdit("balance_edit_in_progress",
			"This group's balance is already being requested. Wait a minute and refresh.")
	}

	if err := s.orderEditor.CommitOrderEdit(ctx, calc.ID, false, balanceStaffNote(plan)); err != nil {
		// The edit may or may not have been applied. The claim stays so the next request
		// checks Shopify and either finishes this edit or redoes it, never both.
		slog.ErrorContext(ctx, "balance edit: commit outcome unknown", "order_id", o.ID, "shipment_id", sh.ID, "error", err)
		return nil, apierror.New(http.StatusBadGateway, "shopify_order_edit_error",
			"Shopify did not confirm the balance. Try again in a few minutes; the app will pick up where it left off.")
	}

	after, err := s.orderEditor.GetOrderBalanceState(ctx, shopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: committed but reloading the order failed", "order_id", o.ID, "shipment_id", sh.ID, "error", err)
		return nil, apierror.New(http.StatusBadGateway, "shopify_order_edit_error",
			"The balance was added to the Shopify order but could not be saved. Try again in a few minutes to finish.")
	}
	known := make(map[string]bool, len(before.LineItems))
	for _, l := range before.LineItems {
		known[shopify.GIDNumericID(l.ID)] = true
	}
	shippingLineID := newShippingLineID(before.ShippingLines, after.ShippingLines, shippingTitle)
	return s.finishOrderEditBalance(ctx, userID, plan, after, known, editPlan.Additions, expected, shippingLineID, locationID)
}

// adoptOrderEditBalance finishes an edit that was committed by an earlier request whose save
// failed. It returns adopted=false when Shopify shows the edit never landed.
func (s *service) adoptOrderEditBalance(ctx context.Context, userID string, plan *groupSecondPaymentPlan, locationID string) (*RequestSecondPaymentResponse, bool, error) {
	o, sh := plan.order, plan.shipment
	state, err := s.orderEditor.GetOrderBalanceState(ctx, *o.ShopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: load Shopify order for adoption failed", "order_id", o.ID, "error", err)
		return nil, false, errShopifyOrderEdit()
	}
	if state.Outstanding <= moneyTolerance {
		return nil, false, nil
	}
	if sh.BalanceExpectedOutstanding == nil || !moneyEqual(state.Outstanding, *sh.BalanceExpectedOutstanding) {
		slog.ErrorContext(ctx, "balance edit: interrupted edit left an unexpected balance",
			"order_id", o.ID, "shipment_id", sh.ID, "shopify_outstanding", state.Outstanding)
		return nil, false, errBalanceEdit("balance_edit_needs_review",
			fmt.Sprintf("The Shopify order owes $%.2f, which is not the balance this group was billing. Check the order in Shopify before retrying.", state.Outstanding))
	}

	known := make(map[string]bool)
	for _, it := range o.Items {
		if it.ShopifyLineItemID != nil {
			known[shopify.GIDNumericID(*it.ShopifyLineItemID)] = true
		}
	}
	existing, err := s.store.GetBalanceLinesByOrderID(ctx, o.ID)
	if err != nil {
		return nil, false, apierror.ErrInternal
	}
	for _, l := range existing {
		known[shopify.GIDNumericID(l.ShopifyLineItemID)] = true
	}
	lines, err := balanceEditLines(plan)
	if err != nil {
		return nil, false, err
	}
	additions := make([]variantAddition, 0, len(lines))
	for _, l := range lines {
		additions = append(additions, variantAddition{
			OrderLineItemID:  l.OrderLineItemID,
			ShopifyVariantID: l.ShopifyVariantID,
			Title:            l.Title,
			Units:            l.Units,
			TargetUnitPrice:  round2(l.TargetUnitPrice),
		})
	}
	shippingTitle := balanceShippingTitle(plan.opts)
	shippingLineID := newShippingLineID(nil, state.ShippingLines, shippingTitle)
	slog.WarnContext(ctx, "balance edit: adopting an edit committed by an earlier request", "order_id", o.ID, "shipment_id", sh.ID)
	resp, err := s.finishOrderEditBalance(ctx, userID, plan, state, known, additions, *sh.BalanceExpectedOutstanding, shippingLineID, locationID)
	return resp, true, err
}

// finishOrderEditBalance saves a committed balance edit and moves the group to "waiting for
// payment". The edit is already live in Shopify, so problems after this point are logged
// and the billing state is saved regardless.
func (s *service) finishOrderEditBalance(
	ctx context.Context,
	userID string,
	plan *groupSecondPaymentPlan,
	after *shopify.OrderBalanceState,
	known map[string]bool,
	additions []variantAddition,
	expected float64,
	shippingLineID string,
	locationID string,
) (*RequestSecondPaymentResponse, error) {
	o, sh := plan.order, plan.shipment
	shopifyOrderID := *o.ShopifyOrderID

	lines, err := matchBalanceLines(after.LineItems, known, additions)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: committed but not every product line was identified; tracking may need a manual fix",
			"order_id", o.ID, "shipment_id", sh.ID, "error", err)
	}

	url := after.PaymentCollectionURL
	if url == "" {
		slog.WarnContext(ctx, "balance edit: Shopify returned no payment link; sending Shopify's invoice email", "order_id", o.ID)
		if err := s.orderEditor.SendOrderInvoice(ctx, shopifyOrderID); err != nil {
			slog.ErrorContext(ctx, "balance edit: Shopify invoice email failed", "order_id", o.ID, "error", err)
		}
	}

	result := OrderEditBalanceResult{
		PaymentURL:          url,
		PrepaidApplied:      plan.prepaidApplied,
		ExpectedOutstanding: expected,
		ShippingLineID:      shippingLineID,
		Lines:               lines,
		SentAt:              time.Now(),
	}
	if err := s.store.CompleteOrderEditBalance(ctx, sh.ID, result); err != nil {
		slog.ErrorContext(ctx, "balance edit: committed in Shopify but saving failed; a retry will adopt it",
			"order_id", o.ID, "shipment_id", sh.ID, "error", err)
		return nil, apierror.ErrInternal
	}

	s.moveBalanceLinesToLocation(ctx, shopifyOrderID, lines, locationID)

	if err := s.markGroupAwaitingSecondPayment(ctx, userID, o.ID, plan.groupItems); err != nil {
		return nil, err
	}
	return &RequestSecondPaymentResponse{InvoiceURL: url}, nil
}

// applyBalanceEdit stages the planned changes in an edit session and returns what the
// customer would owe. Nothing is committed here.
func (s *service) applyBalanceEdit(ctx context.Context, calculatedOrderID string, plan *balanceEditPlan, locationID string) (*shopify.CalculatedOrderTotals, error) {
	var totals *shopify.CalculatedOrderTotals
	for _, r := range plan.Reductions {
		t, err := s.orderEditor.SetOrderEditQuantity(ctx, calculatedOrderID, r.CalculatedLineID, r.NewQuantity)
		if err != nil {
			slog.ErrorContext(ctx, "balance edit: reducing deposit line failed", "line", r.CalculatedLineID, "error", err)
			return nil, errShopifyOrderEdit()
		}
		totals = t
	}

	var aboveCatalog []string
	for _, add := range plan.Additions {
		line, t, err := s.orderEditor.AddOrderEditVariant(ctx, calculatedOrderID, add.ShopifyVariantID, locationID, add.Units)
		if err != nil {
			slog.ErrorContext(ctx, "balance edit: adding product failed", "variant", add.ShopifyVariantID, "error", err)
			return nil, errShopifyOrderEdit()
		}
		totals = t

		catalog := line.OriginalUnitPrice
		switch {
		case add.TargetUnitPrice > catalog+moneyTolerance:
			// A discount cannot raise a price, so the catalog price must be fixed first.
			aboveCatalog = append(aboveCatalog, fmt.Sprintf("%s (website $%.2f, Shopify $%.2f)", add.Title, add.TargetUnitPrice, catalog))
		case catalog-add.TargetUnitPrice > moneyTolerance:
			discount := round2(catalog - add.TargetUnitPrice)
			discounted, t, err := s.orderEditor.AddOrderEditLineDiscount(ctx, calculatedOrderID, line.ID, wholesaleDiscountDescription, discount)
			if err != nil {
				slog.ErrorContext(ctx, "balance edit: wholesale discount failed", "variant", add.ShopifyVariantID, "error", err)
				return nil, errShopifyOrderEdit()
			}
			totals = t
			if !moneyEqual(discounted.DiscountedUnitPrice, add.TargetUnitPrice) {
				slog.ErrorContext(ctx, "balance edit: discounted price is not the website price; edit discarded",
					"variant", add.ShopifyVariantID, "shopify_unit_price", discounted.DiscountedUnitPrice, "target", add.TargetUnitPrice)
				return nil, errBalanceEdit("balance_mismatch",
					fmt.Sprintf("Shopify priced %s at $%.2f instead of $%.2f. Nothing was changed; contact the tech team.", add.Title, discounted.DiscountedUnitPrice, add.TargetUnitPrice))
			}
		}
	}
	if len(aboveCatalog) > 0 {
		return nil, errBalanceEdit("ws_price_above_catalog",
			"The website price is higher than the Shopify price for: "+strings.Join(aboveCatalog, "; ")+
				". Raise the Shopify price to at least the website price, then request the balance again.")
	}

	if plan.Shipping != nil {
		_, t, err := s.orderEditor.AddOrderEditShippingLine(ctx, calculatedOrderID, plan.Shipping.Title, plan.Shipping.Amount)
		if err != nil {
			slog.ErrorContext(ctx, "balance edit: adding shipping failed", "error", err)
			return nil, errShopifyOrderEdit()
		}
		totals = t
	}
	if totals == nil {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Nothing to invoice for this group")
	}
	return totals, nil
}

// checkBatchStock refuses to bill a balance before the batch stock is recorded at the
// location the edit takes stock from. Shopify itself only warns and would let the stock go
// negative.
func (s *service) checkBatchStock(ctx context.Context, lines []balanceEditLine, location *shopify.LocationRef) error {
	need := make(map[string]int)
	titles := make(map[string]string)
	var variantIDs []string
	for _, l := range lines {
		if _, seen := need[l.ShopifyVariantID]; !seen {
			variantIDs = append(variantIDs, l.ShopifyVariantID)
			titles[l.ShopifyVariantID] = l.Title
		}
		need[l.ShopifyVariantID] += l.Units
	}
	refs, err := s.store.GetVariantStockRefs(ctx, variantIDs)
	if err != nil {
		return apierror.ErrInternal
	}
	var itemIDs []string
	for _, v := range variantIDs {
		ref, ok := refs[v]
		if !ok || ref.InventoryItemID == "" {
			return errBalanceEdit("batch_stock_unknown",
				fmt.Sprintf("%s is missing its Shopify inventory link; sync products and try again", titles[v]))
		}
		if ref.Tracked {
			itemIDs = append(itemIDs, inventoryItemGID(ref.InventoryItemID))
		}
	}
	if len(itemIDs) == 0 {
		return nil
	}
	levels, err := s.orderEditor.GetInventoryAtLocation(ctx, itemIDs, location.ID)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: stock check failed", "error", err)
		return errShopifyOrderEdit()
	}
	var short []string
	for _, v := range variantIDs {
		ref := refs[v]
		if !ref.Tracked {
			continue
		}
		level := levels[inventoryItemGID(ref.InventoryItemID)]
		if !level.Tracked {
			continue
		}
		if level.Available < need[v] {
			short = append(short, fmt.Sprintf("%s: need %d, available %d", titles[v], need[v], level.Available))
		}
	}
	if len(short) > 0 {
		name := strings.TrimSpace(location.Name)
		if name == "" {
			name = "the Shopify default location"
		}
		return errBalanceEdit("batch_stock_not_recorded",
			fmt.Sprintf("Record the batch stock in Shopify at %s first. %s", name, strings.Join(short, "; ")))
	}
	return nil
}

// balanceLocation is where a balance edit books its stock: Shopify's primary (default)
// location. The east/west choice made at checkout still decides the shipping rate and the
// warehouse that physically ships; Shopify keeps the inventory at its default location.
func (s *service) balanceLocation(ctx context.Context) (*shopify.LocationRef, error) {
	location, err := s.orderEditor.GetPrimaryLocation(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "balance edit: loading the Shopify default location failed", "error", err)
		return nil, errShopifyOrderEdit()
	}
	if location == nil || location.ID == "" {
		return nil, errBalanceEdit("balance_edit_not_configured", "Shopify did not return a default location for this store")
	}
	return location, nil
}

// moveBalanceLinesToLocation keeps the new product lines at the default location, so the
// stock is taken there and the lines are fulfilled with the app's tracking instead of being
// routed to another location. Best effort: a failure is logged for a manual move.
func (s *service) moveBalanceLinesToLocation(ctx context.Context, shopifyOrderID string, lines []PreorderBalanceLine, locationID string) {
	if len(lines) == 0 || locationID == "" {
		return
	}
	want := make(map[string]bool, len(lines))
	for _, l := range lines {
		want[shopify.GIDNumericID(l.ShopifyLineItemID)] = true
	}
	fos, err := s.shopClient.FetchFulfillmentOrders(ctx, shopifyOrderID)
	if err != nil {
		slog.WarnContext(ctx, "balance edit: could not check where the new lines will ship from", "error", err)
		return
	}
	for _, fo := range fos {
		if fo.Status != "OPEN" || sameShopifyID(fo.AssignedLocationID, locationID) {
			continue
		}
		var move []shopify.FulfillmentOrderLineQuantity
		for _, li := range fo.LineItems {
			if want[shopify.GIDNumericID(li.LineItemID)] && li.RemainingQuantity > 0 {
				move = append(move, shopify.FulfillmentOrderLineQuantity{ID: li.ID, Quantity: li.RemainingQuantity})
			}
		}
		if len(move) == 0 {
			continue
		}
		if err := s.orderEditor.MoveFulfillmentOrderLines(ctx, fo.ID, locationID, move); err != nil {
			slog.ErrorContext(ctx, "balance edit: moving new lines to the default location failed; move them in Shopify",
				"fulfillment_order_id", fo.ID, "from", fo.AssignedLocationName, "error", err)
		}
	}
}

func (s *service) repriceOrderEditBalance(
	ctx context.Context,
	o *Order,
	batchID *string,
	invoiceItems []OrderItem,
	shipment *PreorderShipment,
	finalPrice float64,
	notes string,
) (float64, error) {
	if s.orderEditor == nil {
		return 0, errBalanceEdit("balance_edit_not_configured", "Billing the balance on the original order is not configured")
	}
	if !shipment.AwaitingOrderEditPayment() {
		return 0, errBalanceEdit("balance_edit_in_progress",
			"This group's balance request has not finished yet. Request the balance again, then change the shipping.")
	}
	if o.ShopifyOrderID == nil || *o.ShopifyOrderID == "" {
		return 0, errBalanceEdit("balance_edit_unavailable", "This order has no Shopify order to bill the balance on")
	}
	shopifyOrderID := *o.ShopifyOrderID

	allShipments, err := s.store.GetPreorderShipments(ctx, o.ID)
	if err != nil {
		return 0, apierror.ErrInternal
	}
	prepaidApplied := RepricedPrepaidApplied(allShipments, shipment.ID, finalPrice)
	repriced := *shipment
	repriced.FinalShippingPrice = &finalPrice
	repriced.ShippingNotes = &notes
	plan, err := s.buildGroupSecondPaymentPlan(ctx, o, batchID, invoiceItems, &repriced, prepaidApplied)
	if err != nil {
		return 0, err
	}
	newRemaining := round2(plan.opts.ShippingPrice)
	title := balanceShippingTitle(plan.opts)

	before, err := s.orderEditor.GetOrderBalanceState(ctx, shopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance re-price: load Shopify order failed", "order_id", o.ID, "error", err)
		return 0, errShopifyOrderEdit()
	}
	if before.Outstanding <= moneyTolerance {
		// Paid in Shopify; the payment just has not been recorded here yet.
		return 0, errSecondPaymentAlreadyPaid
	}

	calc, err := s.orderEditor.BeginOrderEdit(ctx, shopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance re-price: begin failed", "order_id", o.ID, "error", err)
		return 0, errShopifyOrderEdit()
	}
	storedLineID := ""
	if shipment.ShopifyShippingLineID != nil {
		storedLineID = *shipment.ShopifyShippingLineID
	}
	current := findShippingLine(calc.ShippingLines, storedLineID, title)
	oldAmount := 0.0
	var totals *shopify.CalculatedOrderTotals
	if current != nil && current.ID != "" {
		oldAmount = current.Price
		t, err := s.orderEditor.RemoveOrderEditShippingLine(ctx, calc.ID, current.ID)
		if err != nil {
			slog.ErrorContext(ctx, "balance re-price: removing old shipping failed", "order_id", o.ID, "error", err)
			return 0, errShopifyOrderEdit()
		}
		totals = t
	} else if storedLineID != "" {
		slog.WarnContext(ctx, "balance re-price: the group's shipping line was not found on the order", "order_id", o.ID, "shipment_id", shipment.ID)
	}
	if newRemaining > 0 {
		_, t, err := s.orderEditor.AddOrderEditShippingLine(ctx, calc.ID, title, newRemaining)
		if err != nil {
			slog.ErrorContext(ctx, "balance re-price: adding new shipping failed", "order_id", o.ID, "error", err)
			return 0, errShopifyOrderEdit()
		}
		totals = t
	}
	if totals == nil {
		// No shipping line before or after: the invoice does not change.
		return prepaidApplied, nil
	}

	expected := round2(before.Outstanding - oldAmount + newRemaining)
	if !moneyEqual(totals.Outstanding, expected) {
		slog.ErrorContext(ctx, "balance re-price: Shopify outstanding differs from app balance; edit discarded",
			"order_id", o.ID, "shipment_id", shipment.ID, "shopify_outstanding", totals.Outstanding, "expected", expected)
		return 0, errBalanceEdit("balance_mismatch",
			fmt.Sprintf("Shopify would bill $%.2f but the app expects $%.2f. Nothing was changed; contact the tech team.", totals.Outstanding, expected))
	}
	note := fmt.Sprintf("Momiji: pre-order shipping re-priced to $%.2f (group %s).", finalPrice, shipment.ID)
	if err := s.orderEditor.CommitOrderEdit(ctx, calc.ID, false, note); err != nil {
		slog.ErrorContext(ctx, "balance re-price: commit outcome unknown", "order_id", o.ID, "shipment_id", shipment.ID, "error", err)
		return 0, apierror.New(http.StatusBadGateway, "shopify_order_edit_error",
			"Shopify did not confirm the new shipping price. Check the order in Shopify before trying again.")
	}

	after, err := s.orderEditor.GetOrderBalanceState(ctx, shopifyOrderID)
	if err != nil {
		slog.ErrorContext(ctx, "balance re-price: committed but reloading the order failed", "order_id", o.ID, "error", err)
		return 0, errShopifyOrderEdit()
	}
	newLineID := newShippingLineID(before.ShippingLines, after.ShippingLines, title)
	if err := s.store.UpdateOrderEditBalanceShipping(ctx, shipment.ID, expected, newLineID, after.PaymentCollectionURL); err != nil {
		if err == ErrPreorderShipmentPaid {
			return 0, errSecondPaymentAlreadyPaid
		}
		slog.ErrorContext(ctx, "balance re-price: committed in Shopify but saving failed", "order_id", o.ID, "shipment_id", shipment.ID, "error", err)
		return 0, apierror.ErrInternal
	}
	return prepaidApplied, nil
}

// balanceEditLines turns a group's invoice lines into the lines a balance edit bills.
func balanceEditLines(plan *groupSecondPaymentPlan) ([]balanceEditLine, error) {
	itemByID := make(map[string]OrderItem, len(plan.groupItems))
	for _, it := range plan.groupItems {
		itemByID[it.ID] = it
	}
	var out []balanceEditLine
	for _, l := range plan.opts.Lines {
		it, ok := itemByID[l.OrderLineItemID]
		if !ok {
			continue
		}
		title := preorder.CleanPreorderDisplayTitle(l.Title)
		target := 0.0
		if it.UnitPrice != nil {
			target = *it.UnitPrice
		}
		deposit := ""
		if it.ShopifyLineItemID != nil {
			deposit = *it.ShopifyLineItemID
		}
		out = append(out, balanceEditLine{
			OrderLineItemID:  it.ID,
			ShopifyVariantID: it.ShopifyVariantID,
			DepositLineID:    deposit,
			Title:            title,
			Units:            l.Quantity,
			TargetUnitPrice:  target,
			AppBalance:       l.Amount,
		})
	}
	if len(out) == 0 && plan.opts.ShippingPrice <= 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Nothing to invoice for this group")
	}
	return out, nil
}

// balanceShippingTitle names a group's remaining-shipping line; the batch name keeps the
// lines of different groups apart on one order.
func balanceShippingTitle(opts preorder.GroupInvoiceOptions) string {
	title := preorder.RemainingShippingLineTitle(opts.ShippingTitle, opts.ShippingPrepaid)
	if batch := strings.TrimSpace(opts.BatchName); batch != "" {
		title += " (" + batch + ")"
	}
	return title
}

func balanceStaffNote(plan *groupSecondPaymentPlan) string {
	label := strings.TrimSpace(plan.opts.BatchName)
	if label == "" {
		label = "pre-order"
	}
	return fmt.Sprintf("Momiji: pre-order balance requested for %s (group %s).", label, plan.shipment.ID)
}

func (s *service) resendOrderEditInvoice(ctx context.Context, o *Order) error {
	if s.orderEditor == nil || o == nil || o.ShopifyOrderID == nil {
		return errBalanceEdit("balance_edit_not_configured", "Billing the balance on the original order is not configured")
	}
	if err := s.orderEditor.SendOrderInvoice(ctx, *o.ShopifyOrderID); err != nil {
		slog.ErrorContext(ctx, "balance edit: Shopify invoice email failed", "order_id", o.ID, "error", err)
		return apierror.New(http.StatusBadGateway, "invoice_send_failed", "Failed to send the invoice email")
	}
	return nil
}

func inventoryItemGID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "gid://") {
		return id
	}
	return "gid://shopify/InventoryItem/" + id
}

func errShopifyOrderEdit() error {
	return apierror.New(http.StatusBadGateway, "shopify_order_edit_error",
		"Shopify could not update the order. Nothing was billed; try again.")
}
