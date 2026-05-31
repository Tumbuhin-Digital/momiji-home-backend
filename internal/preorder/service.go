package preorder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

// OrderUpdater allows preorder service to update order status without circular imports.
type OrderUpdater interface {
	UpdateOrderStatus(ctx context.Context, orderID, financialStatus, fulfillmentStatus string) error
}

// PreorderService defines the settlement state machine operations.
type PreorderService interface {
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderListItemResponse, int64, error)
	GetSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	InvoiceSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	MarkSettlementPaid(ctx context.Context, id string) (*SettlementResponse, error)
	ProcessReminders(ctx context.Context) error
}

type service struct {
	store        PreorderStore
	orderStore   OrderUpdater
	emailService email.NotificationService
}

// NewPreorderService creates the settlement service.
// It requires both PreorderStore (for settlement ops) and OrderUpdater (to update order status).
func NewPreorderService(store PreorderStore, orderStore OrderUpdater, emailService email.NotificationService) PreorderService {
	return &service{
		store:        store,
		orderStore:   orderStore,
		emailService: emailService,
	}
}

func (s *service) ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderListItemResponse, int64, error) {
	rows, total, err := s.store.ListSettlements(ctx, filter)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}

	res := make([]PreorderListItemResponse, len(rows))
	for i, r := range rows {
		var dueDateStr string
		if r.DueDate != nil {
			dueDateStr = r.DueDate.Format("2006-01-02")
		}
		res[i] = PreorderListItemResponse{
			OrderID:          r.OrderID,
			OrderNumber:      r.OrderNumber,
			CustomerEmail:    r.CustomerEmail,
			ItemID:           r.OrderLineItemID,
			Title:            r.Title,
			Quantity:         r.Quantity,
			BalanceDue:       fmt.Sprintf("%.2f", r.BalanceAmount),
			BatchLabel:       r.BatchLabel,
			SettlementStatus: r.SettlementStatus,
			DueDate:          dueDateStr,
		}
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
	go func() {
		bgCtx := context.Background()
		// Re-fetch using ListSettlements to get joined data for email
		rows, _, err := s.store.ListSettlements(bgCtx, SettlementFilter{})
		if err == nil {
			for _, r := range rows {
				if r.ID == id {
					var dueDateStr string
					if r.DueDate != nil {
						dueDateStr = r.DueDate.Format("2006-01-02")
					}
					emailData := email.SettlementEmailData{
						CustomerName:  "Customer",
						ItemTitle:     r.Title,
						BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
						DueDate:       dueDateStr,
						PaymentLink:   "https://momiji-home.com/checkout/settlement/" + id,
					}
					_ = s.emailService.SendInvoice(bgCtx, r.CustomerEmail, emailData)
					break
				}
			}
		}
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
	go func() {
		bgCtx := context.Background()
		// Re-fetch using ListSettlements to get joined data for email
		rows, _, err := s.store.ListSettlements(bgCtx, SettlementFilter{})
		if err == nil {
			for _, r := range rows {
				if r.ID == id {
					emailData := email.SettlementEmailData{
						CustomerName:  "Customer",
						ItemTitle:     r.Title,
						BalanceAmount: fmt.Sprintf("$%.2f", r.BalanceAmount),
					}
					_ = s.emailService.SendSettlementPaid(bgCtx, r.CustomerEmail, emailData)
					break
				}
			}
		}
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
