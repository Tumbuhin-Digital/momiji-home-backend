package preorder

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/xuri/excelize/v2"
)

// OrderUpdater allows preorder service to update order status without circular imports.
type OrderUpdater interface {
	UpdateOrderStatus(ctx context.Context, orderID, aggregateStatus, financialStatus, fulfillmentStatus string) error
	UpdateItemStepByType(ctx context.Context, orderID, itemType string, step int) error
	UpdateItemStatusByType(ctx context.Context, orderID, itemType, status string) error
}

// InvoiceOptions carries optional shipping line data for settlement invoices.
type InvoiceOptions struct {
	ShippingTitle   string
	ShippingPrice   float64
	ShippingNotes   string
	ShippingAddress *shopify.AddressInput
}

// GroupInvoiceLine is a prorated remaining-balance line for one order line slice.
type GroupInvoiceLine struct {
	Title           string
	Amount          float64
	OrderLineItemID string
	Quantity        int
	ShipmentID      string
	OrderID         string
}

// GroupInvoiceOptions creates a Shopify draft invoice for one fulfillment group
// without marking preorder_settlements as invoiced (supports split-line proration).
type GroupInvoiceOptions struct {
	CustomerEmail   string
	CustomerName    string
	OrderID         string
	ShipmentID      string
	BatchName       string // pre-order batch display name for the invoice email header
	ItemCount       int    // total units in this fulfillment group
	Lines           []GroupInvoiceLine
	ShippingTitle   string
	ShippingPrice   float64
	ShippingNotes   string
	ShippingAddress *shopify.AddressInput
}

// GroupInvoiceResult is the draft order created for a group second payment.
type GroupInvoiceResult struct {
	DraftOrderID string
	InvoiceURL   string
}

// PreorderService defines the settlement state machine operations.
type PreorderService interface {
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderGroupResponse, int64, error)
	GetSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	InvoiceSettlements(ctx context.Context, ids []string) ([]SettlementResponse, error)
	InvoiceSettlementsWithShipping(ctx context.Context, ids []string, opts InvoiceOptions) ([]SettlementResponse, error)
	CreateGroupSecondPaymentInvoice(ctx context.Context, opts GroupInvoiceOptions) (*GroupInvoiceResult, error)
	MarkSettlementsPaid(ctx context.Context, ids []string) ([]SettlementResponse, error)
	ProcessReminders(ctx context.Context) error
	ExportPreordersToExcel(ctx context.Context, filter SettlementFilter) ([]byte, error)
	CascadeSettlementPayment(ctx context.Context, orderID string) error
}

type service struct {
	store        PreorderStore
	orderStore   OrderUpdater
	emailService email.NotificationService
	shopClient   shopify.Client
	feURL        string
}

// NewPreorderService creates the settlement service.
// It requires both PreorderStore (for settlement ops) and OrderUpdater (to update order status).
func NewPreorderService(store PreorderStore, orderStore OrderUpdater, emailService email.NotificationService, shopClient shopify.Client, feURL string) PreorderService {
	return &service{
		store:        store,
		orderStore:   orderStore,
		emailService: emailService,
		shopClient:   shopClient,
		feURL:        feURL,
	}
}

func (s *service) ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderGroupResponse, int64, error) {
	rows, total, err := s.store.ListSettlements(ctx, filter)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}

	// Group by Product Title
	groupsMap := make(map[string]*PreorderGroupResponse)
	var orderedTitles []string

	for _, r := range rows {
		var dueDateStr string
		if r.DueDate != nil {
			dueDateStr = r.DueDate.Format("2006-01-02")
		}

		settlement := PreorderGroupSettlement{
			SettlementID:     r.ID,
			OrderID:          r.OrderID,
			OrderNumber:      r.OrderNumber,
			CustomerEmail:    r.CustomerEmail,
			Quantity:         r.Quantity,
			BalanceDue:       fmt.Sprintf("%.2f", r.BalanceAmount),
			BatchLabel:       r.BatchLabel,
			SettlementStatus: r.SettlementStatus,
			DueDate:          dueDateStr,
			CreatedAt:        r.CreatedAt.Format(time.RFC3339),
		}

		if g, exists := groupsMap[r.Title]; exists {
			g.TotalQuantity += r.Quantity
			g.Settlements = append(g.Settlements, settlement)
		} else {
			groupsMap[r.Title] = &PreorderGroupResponse{
				ProductName:   r.Title,
				TotalQuantity: r.Quantity,
				Settlements:   []PreorderGroupSettlement{settlement},
			}
			orderedTitles = append(orderedTitles, r.Title)
		}
	}

	var res []PreorderGroupResponse
	for _, title := range orderedTitles {
		res = append(res, *groupsMap[title])
	}

	return res, total, nil
}

func (s *service) GetSettlement(ctx context.Context, id string) (*SettlementResponse, error) {
	st, err := s.store.GetSettlementByID(ctx, id)
	if err != nil {
		return nil, apierror.ErrNotFound
	}
	res := toResponse(*st)
	return &res, nil
}

// InvoiceSettlements transitions: pending → invoiced for multiple settlements.
// Validates that all settlements belong to the same customer.
// Creates ONE Shopify Draft Order containing all items.
// Sends ONE email to the customer.
func (s *service) InvoiceSettlements(ctx context.Context, ids []string) ([]SettlementResponse, error) {
	return s.InvoiceSettlementsWithShipping(ctx, ids, InvoiceOptions{})
}

// InvoiceSettlementsWithShipping transitions pending → invoiced, optionally adding a shipping line.
func (s *service) InvoiceSettlementsWithShipping(ctx context.Context, ids []string, opts InvoiceOptions) ([]SettlementResponse, error) {
	if len(ids) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No order line item IDs provided")
	}

	var settlements []Settlement
	var customerEmail string
	var customerName string

	// 1. Fetch and validate all settlements
	for i, id := range ids {
		st, err := s.store.GetSettlementByOrderLineItemID(ctx, id)
		if err != nil {
			return nil, apierror.New(http.StatusNotFound, "not_found", fmt.Sprintf("Settlement for OrderLineItemID %s not found", id))
		}

		if st.Status != "pending" {
			return nil, apierror.New(
				http.StatusConflict,
				"invalid_transition",
				fmt.Sprintf("Settlement %s is already %s; only 'pending' settlements can be invoiced", id, st.Status),
			)
		}

		if i == 0 {
			customerEmail = st.CustomerEmail
			customerName = st.CustomerName
		} else {
			if st.CustomerEmail != customerEmail {
				return nil, apierror.New(
					http.StatusBadRequest,
					"invalid_request",
					"All settlements must belong to the same customer to be invoiced together",
				)
			}
		}

		settlements = append(settlements, *st)
	}

	// 2. Prepare Shopify Draft Order and Email Data
	var lineItems []shopify.DraftOrderLineItem
	var emailItemTitles []string
	var emailItems []email.SettlementItemData
	var totalBalance float64

	for _, st := range settlements {
		totalBalance += st.BalanceAmount
		emailItemTitles = append(emailItemTitles, st.Title)
		emailItems = append(emailItems, email.SettlementItemData{
			Title:  st.Title,
			Amount: fmt.Sprintf("$%.2f", st.BalanceAmount),
		})

		lineItems = append(lineItems, shopify.DraftOrderLineItem{
			Title:             fmt.Sprintf("Remaining Balance: %s", st.Title),
			OriginalUnitPrice: fmt.Sprintf("%.2f", st.BalanceAmount),
			Quantity:          1,
			CustomAttributes: []shopify.AttributeInput{
				{Key: "settlement_id", Value: st.ID},
				{Key: "original_order_id", Value: st.OrderID},
			},
		})
	}

	draftInput := shopify.DraftOrderInput{
		Email:     customerEmail,
		LineItems: lineItems,
		CustomAttributes: []shopify.AttributeInput{
			shopify.WholesaleSourceAttribute,
		},
	}

	if opts.ShippingAddress != nil {
		draftInput.ShippingAddress = opts.ShippingAddress
		draftInput.BillingAddress = opts.ShippingAddress
	}

	if opts.ShippingPrice > 0 {
		shippingTitle := shipping.CustomerShippingLineTitle(opts.ShippingTitle)
		lineItems = append(lineItems, shopify.DraftOrderLineItem{
			Title:             shippingTitle,
			OriginalUnitPrice: fmt.Sprintf("%.2f", opts.ShippingPrice),
			Quantity:          1,
			CustomAttributes: []shopify.AttributeInput{
				{Key: "charge_type", Value: "pre_order_shipping"},
				{Key: "original_order_id", Value: settlements[0].OrderID},
			},
		})
		draftInput.LineItems = lineItems
		if opts.ShippingNotes != "" {
			draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
				Key: "shipping_notes", Value: opts.ShippingNotes,
			})
		}
	}

	// 3. Create Shopify Draft Order
	draftRes, err := s.shopClient.CreateDraftOrder(ctx, draftInput)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create shopify draft order for bulk settlements", "error", err)
		return nil, apierror.New(http.StatusBadGateway, "shopify_draft_order_error", "Failed to create Shopify draft order for settlement invoice")
	}
	if draftRes == nil || draftRes.InvoiceUrl == "" {
		slog.ErrorContext(ctx, "shopify draft order missing invoice URL for bulk settlements")
		return nil, apierror.New(http.StatusBadGateway, "shopify_draft_order_error", "Shopify draft order did not return an invoice URL")
	}
	paymentLink := draftRes.InvoiceUrl

	// 4. Update Database (all-or-nothing)
	now := time.Now()
	settlementIDs := make([]string, len(settlements))
	for i := range settlements {
		settlementIDs[i] = settlements[i].ID
	}
	if err := s.store.MarkSettlementsInvoiced(ctx, settlementIDs, draftRes.ID, paymentLink, now); err != nil {
		slog.ErrorContext(ctx, "failed to mark settlements invoiced", "error", err)
		return nil, apierror.ErrInternal
	}

	var responses []SettlementResponse
	for i := range settlements {
		settlements[i].Status = "invoiced"
		settlements[i].InvoicedAt = &now
		settlements[i].ShopifyInvoiceURL = &paymentLink
		settlements[i].ShopifyDraftOrderID = &draftRes.ID
		responses = append(responses, toResponse(settlements[i]))
	}

	// 5. Send ONE Email (only after DB commit succeeds)
	var dueDateStr string
	if settlements[0].DueDate != nil {
		dueDateStr = settlements[0].DueDate.Format("2006-01-02")
	}

	combinedTitle := strings.Join(emailItemTitles, ", ")
	totalDue := totalBalance + opts.ShippingPrice

	go func() {
		bgCtx := context.Background()
		emailData := email.SettlementEmailData{
			CustomerName:   customerName,
			ItemTitle:      combinedTitle,
			Items:          emailItems,
			BalanceAmount:  fmt.Sprintf("$%.2f", totalBalance),
			ShippingAmount: "",
			TotalDue:       fmt.Sprintf("$%.2f", totalDue),
			ShippingNotes:  opts.ShippingNotes,
			DueDate:        dueDateStr,
			PaymentLink:    paymentLink,
		}
		if opts.ShippingPrice > 0 {
			emailData.ShippingAmount = fmt.Sprintf("$%.2f", opts.ShippingPrice)
			emailData.ShippingTitle = shipping.CustomerShippingLineTitle(opts.ShippingTitle)
		}

		if err := s.emailService.SendInvoice(bgCtx, customerEmail, emailData); err != nil {
			slog.Error("failed to send bulk settlement email", "error", err, "email", customerEmail)
		}
	}()

	return responses, nil
}

// CreateGroupSecondPaymentInvoice builds a Shopify draft for one pre-order group
// using explicit prorated amounts. It does not change settlement status.
func (s *service) CreateGroupSecondPaymentInvoice(ctx context.Context, opts GroupInvoiceOptions) (*GroupInvoiceResult, error) {
	if opts.CustomerEmail == "" {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Customer email is required")
	}
	if opts.ShipmentID == "" || opts.OrderID == "" {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Shipment and order IDs are required")
	}
	if len(opts.Lines) == 0 && opts.ShippingPrice <= 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Invoice must include balance lines or shipping")
	}

	var lineItems []shopify.DraftOrderLineItem
	var emailItems []email.SettlementItemData
	var emailTitles []string
	var totalBalance float64

	for _, line := range opts.Lines {
		if line.Amount <= 0 {
			continue
		}
		totalBalance += line.Amount
		displayTitle := cleanPreorderDisplayTitle(line.Title)
		title := displayTitle
		if line.Quantity > 0 {
			title = fmt.Sprintf("Remaining balance — %s (%d pcs)", displayTitle, line.Quantity)
		} else {
			title = fmt.Sprintf("Remaining balance — %s", displayTitle)
		}
		emailTitles = append(emailTitles, title)
		emailItems = append(emailItems, email.SettlementItemData{
			Title:  title,
			Amount: fmt.Sprintf("$%.2f", line.Amount),
		})
		lineItems = append(lineItems, shopify.DraftOrderLineItem{
			Title:             title,
			OriginalUnitPrice: fmt.Sprintf("%.2f", line.Amount),
			Quantity:          1,
			CustomAttributes: []shopify.AttributeInput{
				{Key: "charge_type", Value: "pre_order_group_balance"},
				{Key: "shipment_id", Value: opts.ShipmentID},
				{Key: "original_order_id", Value: opts.OrderID},
				{Key: "order_line_item_id", Value: line.OrderLineItemID},
			},
		})
	}

	draftInput := shopify.DraftOrderInput{
		Email:     opts.CustomerEmail,
		LineItems: lineItems,
		CustomAttributes: []shopify.AttributeInput{
			shopify.WholesaleSourceAttribute,
			{Key: "charge_type", Value: "pre_order_group_invoice"},
			{Key: "shipment_id", Value: opts.ShipmentID},
			{Key: "original_order_id", Value: opts.OrderID},
		},
	}

	if opts.ShippingAddress != nil {
		draftInput.ShippingAddress = opts.ShippingAddress
		draftInput.BillingAddress = opts.ShippingAddress
	}

	if opts.ShippingPrice > 0 {
		shippingTitle := shipping.CustomerShippingLineTitle(opts.ShippingTitle)
		lineItems = append(lineItems, shopify.DraftOrderLineItem{
			Title:             shippingTitle,
			OriginalUnitPrice: fmt.Sprintf("%.2f", opts.ShippingPrice),
			Quantity:          1,
			CustomAttributes: []shopify.AttributeInput{
				{Key: "charge_type", Value: "pre_order_shipping"},
				{Key: "shipment_id", Value: opts.ShipmentID},
				{Key: "original_order_id", Value: opts.OrderID},
			},
		})
		draftInput.LineItems = lineItems
		if opts.ShippingNotes != "" {
			draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
				Key: "shipping_notes", Value: opts.ShippingNotes,
			})
		}
	}

	draftRes, err := s.shopClient.CreateDraftOrder(ctx, draftInput)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create shopify draft order for group invoice", "error", err)
		return nil, apierror.New(http.StatusBadGateway, "shopify_draft_order_error", "Failed to create Shopify draft order for group invoice")
	}
	if draftRes == nil || draftRes.InvoiceUrl == "" {
		return nil, apierror.New(http.StatusBadGateway, "shopify_draft_order_error", "Shopify draft order did not return an invoice URL")
	}

	paymentLink := draftRes.InvoiceUrl
	totalDue := totalBalance + opts.ShippingPrice
	customerName := opts.CustomerName
	if customerName == "" {
		customerName = "Customer"
	}
	combinedTitle := strings.Join(emailTitles, ", ")
	invoiceHeading := groupInvoiceHeading(opts.BatchName, opts.ItemCount)

	go func() {
		bgCtx := context.Background()
		emailData := email.SettlementEmailData{
			CustomerName:   customerName,
			ItemTitle:      combinedTitle,
			InvoiceHeading: invoiceHeading,
			Items:          emailItems,
			BalanceAmount:  fmt.Sprintf("$%.2f", totalBalance),
			ShippingAmount: "",
			TotalDue:       fmt.Sprintf("$%.2f", totalDue),
			ShippingNotes:  opts.ShippingNotes,
			PaymentLink:    paymentLink,
		}
		if opts.ShippingPrice > 0 {
			emailData.ShippingAmount = fmt.Sprintf("$%.2f", opts.ShippingPrice)
			emailData.ShippingTitle = shipping.CustomerShippingLineTitle(opts.ShippingTitle)
		}
		if err := s.emailService.SendInvoice(bgCtx, opts.CustomerEmail, emailData); err != nil {
			slog.Error("failed to send group settlement email", "error", err, "email", opts.CustomerEmail)
		}
	}()

	return &GroupInvoiceResult{
		DraftOrderID: draftRes.ID,
		InvoiceURL:   paymentLink,
	}, nil
}

// MarkSettlementsPaid transitions: invoiced → paid for multiple settlements
// Checks if ALL settlements for the associated order are paid and updates order status.
func (s *service) MarkSettlementsPaid(ctx context.Context, ids []string) ([]SettlementResponse, error) {
	if len(ids) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No order line item IDs provided")
	}

	var settlements []Settlement
	var customerEmail string
	var customerName string
	var orderID string

	// 1. Fetch and validate
	for i, id := range ids {
		st, err := s.store.GetSettlementByOrderLineItemID(ctx, id)
		if err != nil {
			return nil, apierror.New(http.StatusNotFound, "not_found", fmt.Sprintf("Settlement for OrderLineItemID %s not found", id))
		}

		if st.Status != "invoiced" {
			return nil, apierror.New(
				http.StatusConflict,
				"invalid_transition",
				fmt.Sprintf("Settlement %s is already %s; only 'invoiced' settlements can be marked paid", id, st.Status),
			)
		}

		if i == 0 {
			customerEmail = st.CustomerEmail
			customerName = st.CustomerName
			orderID = st.OrderID
		} else {
			if st.CustomerEmail != customerEmail {
				return nil, apierror.New(
					http.StatusBadRequest,
					"invalid_request",
					"All settlements must belong to the same customer to be marked paid together",
				)
			}
		}

		settlements = append(settlements, *st)
	}

	// 2. Update Database
	now := time.Now()
	var responses []SettlementResponse
	var totalBalance float64
	var emailItemTitles []string
	var emailItems []email.SettlementItemData

	for i := range settlements {
		if err := s.store.UpdateSettlementStatus(ctx, settlements[i].ID, "paid", &now); err != nil {
			slog.ErrorContext(ctx, "failed to update settlement to paid", "id", settlements[i].ID, "error", err)
			continue
		}
		
		totalBalance += settlements[i].BalanceAmount
		emailItemTitles = append(emailItemTitles, settlements[i].Title)
		emailItems = append(emailItems, email.SettlementItemData{
			Title:  settlements[i].Title,
			Amount: fmt.Sprintf("$%.2f", settlements[i].BalanceAmount),
		})
		
		settlements[i].Status = "paid"
		settlements[i].PaidAt = &now
		responses = append(responses, toResponse(settlements[i]))
	}

	// 3. Cascade: if all settlements for this order are paid, mark the order as paid
	if orderID != "" {
		if err := s.checkAndUpdateOrderStatus(ctx, orderID); err != nil {
			_ = err
		}
	}

	// 4. Send ONE Email
	combinedTitle := strings.Join(emailItemTitles, ", ")

	go func() {
		bgCtx := context.Background()
		emailData := email.SettlementEmailData{
			CustomerName:  customerName,
			ItemTitle:     combinedTitle,
			Items:         emailItems,
			BalanceAmount: fmt.Sprintf("$%.2f", totalBalance),
		}
		_ = s.emailService.SendSettlementPaid(bgCtx, customerEmail, emailData)
	}()

	return responses, nil
}

// checkAndUpdateOrderStatus updates orders.aggregate_status to 'paid'
// if every settlement for the order is in 'paid' state.
func (s *service) checkAndUpdateOrderStatus(ctx context.Context, orderID string) error {
	allPaid, err := s.store.AllSettlementsPaid(ctx, orderID)
	if err != nil {
		return err
	}
	if allPaid {
		return s.orderStore.UpdateOrderStatus(ctx, orderID, "paid", "paid", "pending")
	}
	return nil
}

// CascadeSettlementPayment advances pre-order fulfillment after all settlements are paid.
func (s *service) CascadeSettlementPayment(ctx context.Context, orderID string) error {
	allPaid, err := s.store.AllSettlementsPaid(ctx, orderID)
	if err != nil {
		return err
	}
	if !allPaid {
		return nil
	}

	if err := s.orderStore.UpdateOrderStatus(ctx, orderID, "paid", "paid", "pending"); err != nil {
		return err
	}
	if err := s.orderStore.UpdateItemStatusByType(ctx, orderID, "pre_order", "paid"); err != nil {
		return err
	}
	return s.orderStore.UpdateItemStepByType(ctx, orderID, "pre_order", 4)
}

func (s *service) ProcessReminders(ctx context.Context) error {
	// D+3 Reminder
	rowsD3, err := s.store.GetSettlementsForReminder(ctx, 3)
	if err == nil {
		for _, r := range rowsD3 {
			if r.ShopifyInvoiceURL == nil || *r.ShopifyInvoiceURL == "" {
				slog.WarnContext(ctx, "skipping D+3 reminder: settlement missing shopify invoice URL", "settlement_id", r.ID)
				continue
			}
			var dueDateStr string
			if r.DueDate != nil {
				dueDateStr = r.DueDate.Format("2006-01-02")
			}
			emailData := email.SettlementEmailData{
				CustomerName:  r.CustomerName,
				ItemTitle:     r.Title,
				BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
				DueDate:       dueDateStr,
				PaymentLink:   *r.ShopifyInvoiceURL,
			}
			_ = s.emailService.SendReminder(ctx, r.CustomerEmail, emailData)
		}
	}

	// D+6 Final Reminder
	rowsD6, err := s.store.GetSettlementsForReminder(ctx, 6)
	if err == nil {
		for _, r := range rowsD6 {
			if r.ShopifyInvoiceURL == nil || *r.ShopifyInvoiceURL == "" {
				slog.WarnContext(ctx, "skipping D+6 reminder: settlement missing shopify invoice URL", "settlement_id", r.ID)
				continue
			}
			var dueDateStr string
			if r.DueDate != nil {
				dueDateStr = r.DueDate.Format("2006-01-02")
			}
			emailData := email.SettlementEmailData{
				CustomerName:  r.CustomerName,
				ItemTitle:     r.Title,
				BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
				DueDate:       dueDateStr,
				PaymentLink:   *r.ShopifyInvoiceURL,
			}
			_ = s.emailService.SendReminder(ctx, r.CustomerEmail, emailData)
		}
	}

	// D+7 Expired
	rowsD7, err := s.store.GetSettlementsForReminder(ctx, 7)
	if err == nil {
		for _, r := range rowsD7 {
			// Mark expired and trigger email
			now := time.Now()
			if err := s.store.UpdateSettlementStatus(ctx, r.ID, "expired", &now); err == nil {
				emailData := email.SettlementEmailData{
					CustomerName:  r.CustomerName,
					ItemTitle:     r.Title,
					BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
				}
				_ = s.emailService.SendExpired(ctx, r.CustomerEmail, emailData)

				// Calculate 80% refund of the deposit (balanceAmount is equal to deposit)
				if r.ShopifyOrderID != nil && *r.ShopifyOrderID != "" {
					refundAmount := r.BalanceAmount * 0.8
					refundErr := s.shopClient.CreateRefund(ctx, *r.ShopifyOrderID, refundAmount, "USD", "Pre-order expired — auto-refund 80%")
					if refundErr != nil {
						// Log error but don't fail the expiry process
						fmt.Printf("Failed to refund expired settlement %s: %v\n", r.ID, refundErr)
					}
				}
			}
		}
	}

	return nil
}

func toResponse(st Settlement) SettlementResponse {
	return SettlementResponse{
		ID:                  st.ID,
		OrderLineItemID:     st.OrderLineItemID,
		OrderID:             st.OrderID,
		Status:              st.Status,
		BalanceAmount:       st.BalanceAmount,
		DueDate:             st.DueDate,
		InvoicedAt:          st.InvoicedAt,
		PaidAt:              st.PaidAt,
		ShopifyInvoiceURL:   st.ShopifyInvoiceURL,
		ShopifyDraftOrderID: st.ShopifyDraftOrderID,
		CreatedAt:           st.CreatedAt,
	}
}

func (s *service) ExportPreordersToExcel(ctx context.Context, filter SettlementFilter) ([]byte, error) {
	rows, err := s.store.GetAllSettlementsForExport(ctx, filter)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	sheetName := "Preorder List"
	f.SetSheetName("Sheet1", sheetName)
	
	headers := []string{"Order ID", "Order Number", "Product Name", "Customer Email", "Quantity", "Balance Due", "Status", "Due Date", "Batch Label"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
	}

	rowNum := 2
	for _, r := range rows {
		var dueDateStr string
		if r.DueDate != nil {
			dueDateStr = r.DueDate.Format("2006-01-02")
		}
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowNum), r.OrderID)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowNum), r.OrderNumber)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowNum), r.Title)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", rowNum), r.CustomerEmail)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", rowNum), r.Quantity)
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", rowNum), fmt.Sprintf("%.2f", r.BalanceAmount))
		f.SetCellValue(sheetName, fmt.Sprintf("G%d", rowNum), r.SettlementStatus)
		f.SetCellValue(sheetName, fmt.Sprintf("H%d", rowNum), dueDateStr)
		f.SetCellValue(sheetName, fmt.Sprintf("I%d", rowNum), r.BatchLabel)
		rowNum++
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create header style: %w", err)
	}
	if err := f.SetCellStyle(sheetName, "A1", "I1", headerStyle); err != nil {
		return nil, fmt.Errorf("failed to apply header style: %w", err)
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
		return nil, fmt.Errorf("failed to freeze header row: %w", err)
	}

	cols := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
	for _, col := range cols {
		maxLen := 0
		for row := 1; row < rowNum; row++ {
			val, _ := f.GetCellValue(sheetName, fmt.Sprintf("%s%d", col, row))
			if l := len(val); l > maxLen {
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
		if err := f.SetColWidth(sheetName, col, col, width); err != nil {
			return nil, fmt.Errorf("failed to set column width: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write excel file: %w", err)
	}
	return buf.Bytes(), nil
}

func cleanPreorderDisplayTitle(title string) string {
	title = strings.TrimSpace(title)
	for _, prefix := range []string{"[PREORDER] ", "[PREORDER]", "[Preorder] ", "[preorder] "} {
		if strings.HasPrefix(title, prefix) {
			title = strings.TrimSpace(strings.TrimPrefix(title, prefix))
			break
		}
	}
	return title
}

// groupInvoiceHeading builds the second-payment email header, e.g.
// "Pre-Order Batch - September 2026 (4 items)".
func groupInvoiceHeading(batchName string, itemCount int) string {
	batchName = strings.TrimSpace(batchName)
	if batchName == "" {
		if itemCount > 0 {
			return fmt.Sprintf("Pre-Order (%d items)", itemCount)
		}
		return "Pre-order balance invoice"
	}
	if itemCount > 0 {
		return fmt.Sprintf("Pre-Order %s (%d items)", batchName, itemCount)
	}
	return fmt.Sprintf("Pre-Order %s", batchName)
}
