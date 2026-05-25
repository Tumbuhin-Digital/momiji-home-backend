package preorder

import (
	"context"
	"net/http"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

// OrderUpdater allows preorder service to update order status without circular imports.
type OrderUpdater interface {
	UpdateOrderStatus(ctx context.Context, orderID, financialStatus, fulfillmentStatus string) error
}

// PreorderService defines the settlement state machine operations.
type PreorderService interface {
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]SettlementResponse, int64, error)
	GetSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	InvoiceSettlement(ctx context.Context, id string) (*SettlementResponse, error)
	MarkSettlementPaid(ctx context.Context, id string) (*SettlementResponse, error)
}

type service struct {
	store      PreorderStore
	orderStore OrderUpdater
}

// NewPreorderService creates the settlement service.
// It requires both PreorderStore (for settlement ops) and OrderUpdater (to update order status).
func NewPreorderService(store PreorderStore, orderStore OrderUpdater) PreorderService {
	return &service{
		store:      store,
		orderStore: orderStore,
	}
}

func (s *service) ListSettlements(ctx context.Context, filter SettlementFilter) ([]SettlementResponse, int64, error) {
	settlements, total, err := s.store.ListSettlements(ctx, filter)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}

	res := make([]SettlementResponse, len(settlements))
	for i, st := range settlements {
		res[i] = toResponse(st)
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
