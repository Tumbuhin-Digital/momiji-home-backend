package preorder

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/xuri/excelize/v2"
)

// OrderUpdater allows preorder service to update order status without circular imports.
type OrderUpdater interface {
	UpdateOrderStatus(ctx context.Context, orderID, financialStatus, fulfillmentStatus string) error
}

// PreorderService defines the settlement state machine operations.
type PreorderService interface {
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderGroupResponse, int64, error)
	GetSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	InvoiceSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	MarkSettlementPaid(ctx context.Context, id string) (*SettlementResponse, error)
	ProcessReminders(ctx context.Context) error
	ExportPreordersToExcel(ctx context.Context, filter SettlementFilter) ([]byte, error)
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

// InvoiceSettlement transitions: pending → invoiced
// Returns 409 Conflict if the settlement is not in 'pending' state.
func (s *service) InvoiceSettlement(ctx context.Context, id string) (*SettlementResponse, error) {
	st, err := s.store.GetSettlementByID(ctx, id)
	if err != nil {
		return nil, apierror.ErrNotFound
	}

	if st.Status != "pending" {
		return nil, apierror.New(
			http.StatusConflict,
			"invalid_transition",
			"Settlement is already "+st.Status+"; only 'pending' settlements can be invoiced",
		)
	}

	now := time.Now()
	if err := s.store.UpdateSettlementStatus(ctx, id, "invoiced", &now); err != nil {
		return nil, apierror.ErrInternal
	}

	// TODO Phase 8: trigger pelunasan invoice email to customer here

	st.Status = "invoiced"
	st.InvoicedAt = &now
	res := toResponse(*st)

	// Trigger email in background
	settlementID := id
	customerEmail := st.CustomerEmail
	itemTitle := st.Title
	balanceAmount := st.BalanceAmount
	var dueDateStr string
	if st.DueDate != nil {
		dueDateStr = st.DueDate.Format("2006-01-02")
	}

	go func() {
		bgCtx := context.Background()
		emailData := email.SettlementEmailData{
			CustomerName:  "Customer",
			ItemTitle:     itemTitle,
			BalanceAmount: fmt.Sprintf("$%.2f", balanceAmount),
			DueDate:       dueDateStr,
			PaymentLink:   s.feURL + "/checkout/settlement/" + settlementID,
		}
		_ = s.emailService.SendInvoice(bgCtx, customerEmail, emailData)
	}()

	return &res, nil
}

// MarkSettlementPaid transitions: invoiced → paid
// Returns 409 Conflict if the settlement is not in 'invoiced' state.
// After marking paid, checks if ALL settlements for the order are paid and updates order status.
func (s *service) MarkSettlementPaid(ctx context.Context, id string) (*SettlementResponse, error) {
	st, err := s.store.GetSettlementByID(ctx, id)
	if err != nil {
		return nil, apierror.ErrNotFound
	}

	if st.Status != "invoiced" {
		return nil, apierror.New(
			http.StatusConflict,
			"invalid_transition",
			"Settlement is already "+st.Status+"; only 'invoiced' settlements can be marked paid",
		)
	}

	now := time.Now()
	if err := s.store.UpdateSettlementStatus(ctx, id, "paid", &now); err != nil {
		return nil, apierror.ErrInternal
	}

	// Cascade: if all settlements for this order are paid, mark the order as paid
	if err := s.checkAndUpdateOrderStatus(ctx, st.OrderID); err != nil {
		// Non-fatal: log in Phase 8, don't fail the request
		_ = err
	}

	st.Status = "paid"
	st.PaidAt = &now
	res := toResponse(*st)

	// Trigger email in background
	customerEmail := st.CustomerEmail
	itemTitle := st.Title
	balanceAmount := st.BalanceAmount

	go func() {
		bgCtx := context.Background()
		emailData := email.SettlementEmailData{
			CustomerName:  "Customer",
			ItemTitle:     itemTitle,
			BalanceAmount: fmt.Sprintf("$%.2f", balanceAmount),
		}
		_ = s.emailService.SendSettlementPaid(bgCtx, customerEmail, emailData)
	}()

	return &res, nil
}

// checkAndUpdateOrderStatus updates orders.aggregate_status to 'paid'
// if every settlement for the order is in 'paid' state.
func (s *service) checkAndUpdateOrderStatus(ctx context.Context, orderID string) error {
	allPaid, err := s.store.AllSettlementsPaid(ctx, orderID)
	if err != nil {
		return err
	}
	if allPaid {
		return s.orderStore.UpdateOrderStatus(ctx, orderID, "paid", "pending")
	}
	return nil
}

func (s *service) ProcessReminders(ctx context.Context) error {
	// D+3 Reminder
	rowsD3, err := s.store.GetSettlementsForReminder(ctx, 3)
	if err == nil {
		for _, r := range rowsD3 {
			var dueDateStr string
			if r.DueDate != nil {
				dueDateStr = r.DueDate.Format("2006-01-02")
			}
			emailData := email.SettlementEmailData{
				CustomerName:  "Customer",
				ItemTitle:     r.Title,
				BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
				DueDate:       dueDateStr,
				PaymentLink:   "https://momiji-home.com/checkout/settlement/" + r.ID,
			}
			_ = s.emailService.SendReminder(ctx, r.CustomerEmail, emailData)
		}
	}

	// D+6 Final Reminder
	rowsD6, err := s.store.GetSettlementsForReminder(ctx, 6)
	if err == nil {
		for _, r := range rowsD6 {
			var dueDateStr string
			if r.DueDate != nil {
				dueDateStr = r.DueDate.Format("2006-01-02")
			}
			emailData := email.SettlementEmailData{
				CustomerName:  "Customer",
				ItemTitle:     r.Title,
				BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
				DueDate:       dueDateStr,
				PaymentLink:   "https://momiji-home.com/checkout/settlement/" + r.ID,
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
					CustomerName:  "Customer",
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
		ID:              st.ID,
		OrderLineItemID: st.OrderLineItemID,
		OrderID:         st.OrderID,
		Status:          st.Status,
		BalanceAmount:   st.BalanceAmount,
		DueDate:         st.DueDate,
		InvoicedAt:      st.InvoicedAt,
		PaidAt:          st.PaidAt,
		CreatedAt:       st.CreatedAt,
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

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write excel file: %w", err)
	}
	return buf.Bytes(), nil
}
