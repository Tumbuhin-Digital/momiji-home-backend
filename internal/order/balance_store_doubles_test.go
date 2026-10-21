package order

import "context"

// No-op BalanceEditStore methods for the shared shipping test store: tests that do not
// exercise order-edit balances see no balance lines and no stock records.

func (s *updateShippingWarehouseStore) GetVariantStockRefs(context.Context, []string) (map[string]VariantStockRef, error) {
	return map[string]VariantStockRef{}, nil
}

func (s *updateShippingWarehouseStore) ClaimOrderEditBalance(context.Context, string, float64) (bool, error) {
	return true, nil
}

func (s *updateShippingWarehouseStore) ReleaseOrderEditBalanceClaim(context.Context, string) error {
	return nil
}

func (s *updateShippingWarehouseStore) CompleteOrderEditBalance(context.Context, string, OrderEditBalanceResult) error {
	return nil
}

func (s *updateShippingWarehouseStore) UpdateOrderEditBalanceShipping(context.Context, string, float64, string, string) error {
	return nil
}

func (s *updateShippingWarehouseStore) GetBalanceLinesByOrderID(context.Context, string) ([]PreorderBalanceLine, error) {
	return nil, nil
}

func (s *updateShippingWarehouseStore) ListAwaitingOrderEditBalances(context.Context) ([]PreorderShipment, error) {
	return nil, nil
}
