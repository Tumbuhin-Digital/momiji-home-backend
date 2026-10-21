package webhook

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// A pre-order balance billed by editing the deposit order is paid on that same order, so
// no new order (and no orders/paid for a new order) marks it. It is recognised instead when
// the deposit order no longer owes anything, from a webhook on that order or from the
// periodic reconciliation. See docs/prd/pre-order-revisi-satu-order.md.

// balancePaidTolerance absorbs float noise on whole-cent amounts.
const balancePaidTolerance = 0.005

func (s *service) SetOrderEditor(editor shopify.OrderEditor) {
	s.orderEditor = editor
}

// optionalLineItemGID stores the Shopify line an order item was bought on, so a balance
// edit can find its deposit line without guessing.
func optionalLineItemGID(id int64) *string {
	gid := shopify.LineItemGID(id)
	if gid == "" {
		return nil
	}
	return &gid
}

// ReconcileOrderEditBalance marks the order's awaiting balance group paid once Shopify shows
// nothing left to pay. Only one group can await payment per order, so the payment belongs
// to it. Safe to call repeatedly.
func (s *service) ReconcileOrderEditBalance(ctx context.Context, o *order.Order) error {
	if o == nil || o.ShopifyOrderID == nil || *o.ShopifyOrderID == "" || s.orderEditor == nil {
		return nil
	}
	shipments, err := s.orderStore.GetPreorderShipments(ctx, o.ID)
	if err != nil {
		return err
	}
	var awaiting []order.PreorderShipment
	for _, sh := range shipments {
		if sh.AwaitingOrderEditPayment() {
			awaiting = append(awaiting, sh)
		}
	}
	if len(awaiting) == 0 {
		return nil
	}

	state, err := s.orderEditor.GetOrderBalanceState(ctx, *o.ShopifyOrderID)
	if err != nil {
		return err
	}
	if state.Outstanding > balancePaidTolerance {
		return nil
	}

	now := time.Now()
	for _, sh := range awaiting {
		slog.InfoContext(ctx, "Pre-order balance paid on the deposit order",
			slog.String("order_id", o.ID),
			slog.String("shipment_id", sh.ID),
			slog.String("financial_status", state.FinancialStatus))
		if err := s.orderStore.MarkPreorderShipmentInvoicePaid(ctx, sh.ID, now); err != nil {
			return err
		}
	}
	return s.handleSettlementOnlyPayment(ctx, o.ID)
}

// ReconcilePendingOrderEditBalances checks every balance still awaiting payment. It backs up
// the webhooks, which Shopify may not send (or may send before the payment settles).
func (s *service) ReconcilePendingOrderEditBalances(ctx context.Context) error {
	if s.orderEditor == nil {
		return nil
	}
	shipments, err := s.orderStore.ListAwaitingOrderEditBalances(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, sh := range shipments {
		if seen[sh.OrderID] {
			continue
		}
		seen[sh.OrderID] = true
		o, err := s.orderStore.GetOrder(ctx, sh.OrderID, "")
		if err != nil || o == nil {
			slog.WarnContext(ctx, "balance reconciliation: order not loaded", slog.String("order_id", sh.OrderID), slog.Any("error", err))
			continue
		}
		if err := s.ReconcileOrderEditBalance(ctx, o); err != nil {
			slog.WarnContext(ctx, "balance reconciliation failed", slog.String("order_id", sh.OrderID), slog.Any("error", err))
		}
	}
	return nil
}

// HandleOrderUpdated reacts to any change on a Momiji order; a balance payment on an edited
// deposit order arrives as one of these.
func (s *service) HandleOrderUpdated(ctx context.Context, payload ShopifyOrderWebhook) error {
	if !isWholesaleMomijiOrder(payload) || payload.ID == 0 {
		return nil
	}
	o, err := s.orderStore.GetOrderByShopifyID(ctx, strconv.FormatInt(payload.ID, 10))
	if err != nil || o == nil {
		return err
	}
	if err := s.ReconcileOrderEditBalance(ctx, o); err != nil {
		// The periodic reconciliation retries; failing the webhook would only make Shopify
		// resend a payload that carries nothing new.
		slog.WarnContext(ctx, "order updated: balance reconciliation failed", slog.String("order_id", o.ID), slog.Any("error", err))
	}
	return nil
}
