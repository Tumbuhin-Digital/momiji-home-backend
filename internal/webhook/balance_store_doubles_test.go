package webhook

import (
	"context"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
)

// No-op order.BalanceEditStore methods for the shared webhook test store; tests that care
// about order-edit balances override them.

func (s *fulfillmentTestStore) GetVariantStockRefs(context.Context, []string) (map[string]order.VariantStockRef, error) {
	return map[string]order.VariantStockRef{}, nil
}

func (s *fulfillmentTestStore) ClaimOrderEditBalance(context.Context, string, float64) (bool, error) {
	return true, nil
}

func (s *fulfillmentTestStore) ReleaseOrderEditBalanceClaim(context.Context, string) error {
	return nil
}

func (s *fulfillmentTestStore) CompleteOrderEditBalance(context.Context, string, order.OrderEditBalanceResult) error {
	return nil
}

func (s *fulfillmentTestStore) UpdateOrderEditBalanceShipping(context.Context, string, float64, string, string) error {
	return nil
}

func (s *fulfillmentTestStore) GetBalanceLinesByOrderID(context.Context, string) ([]order.PreorderBalanceLine, error) {
	return nil, nil
}

func (s *fulfillmentTestStore) ListAwaitingOrderEditBalances(context.Context) ([]order.PreorderShipment, error) {
	return nil, nil
}
