package cart

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type reconcileProductStub struct {
	variants map[string]*product.VariantDTO
}

func (s *reconcileProductStub) GetVariantByID(_ context.Context, variantID string) (*product.VariantDTO, error) {
	if v, ok := s.variants[variantID]; ok {
		return v, nil
	}
	return nil, apierror.ErrNotFound
}
func (s *reconcileProductStub) GetProducts(context.Context, product.ProductQuery) ([]product.ProductDTO, int64, error) {
	return nil, 0, nil
}
func (s *reconcileProductStub) SyncFromShopify(context.Context) error { return nil }
func (s *reconcileProductStub) GetProductByID(context.Context, string) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) GetVariantsByProductID(context.Context, string) ([]product.VariantDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) UpdateProductStatus(context.Context, string, string) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) UpdateVariantStatus(context.Context, string, string) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) UpdateVariantBatchLabel(context.Context, string, string, *string) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) UpdateVariantBatchLabelByVariantID(context.Context, string, string) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) UpdateVariantPrice(context.Context, string, *float64) error {
	return nil
}
func (s *reconcileProductStub) UpdateVariantLtl(context.Context, string, bool) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) LinkCustomVariantSKU(context.Context, string, string) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) GetAllVariants(context.Context) ([]product.ProductVariant, error) {
	return nil, nil
}
func (s *reconcileProductStub) BulkUpdateDimensions(context.Context, []product.DimensionUpdateInput) (product.BulkUpdateDimensionsResult, error) {
	return product.BulkUpdateDimensionsResult{}, nil
}
func (s *reconcileProductStub) ValidateVariantActive(context.Context, string) error { return nil }
func (s *reconcileProductStub) CreateCustomProduct(context.Context, product.CreateCustomProductInput) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *reconcileProductStub) SyncInventoryQuantities(context.Context, map[string]int) error {
	return nil
}

type reconcileCartStore struct {
	cart     *Cart
	upserts  []struct{ ship, pre int }
	lastShip int
	lastPre  int
}

func (s *reconcileCartStore) GetCart(context.Context, *string, *string) (*Cart, error) {
	return s.cart, nil
}
func (s *reconcileCartStore) CreateCart(context.Context, *Cart) error { return nil }
func (s *reconcileCartStore) AddItem(context.Context, *CartItemModel) error {
	return nil
}
func (s *reconcileCartStore) GetVariantQtyInCart(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *reconcileCartStore) UpdateItemQuantity(context.Context, string, int) error {
	return nil
}
func (s *reconcileCartStore) UpdateItemUnitPrice(context.Context, string, float64) error {
	return nil
}
func (s *reconcileCartStore) RemoveItem(context.Context, string) error { return nil }
func (s *reconcileCartStore) ClearCart(context.Context, string) error  { return nil }
func (s *reconcileCartStore) MergeCarts(context.Context, string, string) error {
	return nil
}
func (s *reconcileCartStore) GetVariantItemsInCart(context.Context, string, string) ([]CartItemModel, error) {
	return nil, nil
}
func (s *reconcileCartStore) UpsertVariantItems(_ context.Context, _, _ string, shipReadyQty, preOrderQty int, _ float64) error {
	s.lastShip = shipReadyQty
	s.lastPre = preOrderQty
	s.upserts = append(s.upserts, struct{ ship, pre int }{shipReadyQty, preOrderQty})
	return nil
}

func TestReconcileShipReadyAgainstInventory_MovesShortageToPreorder(t *testing.T) {
	t.Parallel()

	sku := "SKU-1"
	store := &reconcileCartStore{
		cart: &Cart{
			ID: "cart-1",
			Items: []CartItemModel{
				{ShopifyVariantID: "gid://v/1", FulfillmentType: "ship_ready", Quantity: 5},
				{ShopifyVariantID: "gid://v/1", FulfillmentType: "pre_order", Quantity: 2},
			},
		},
	}
	svc := &service{
		store: store,
		productService: &reconcileProductStub{variants: map[string]*product.VariantDTO{
			"gid://v/1": {
				ID:               "gid://v/1",
				Title:            "Stool",
				SKU:              &sku,
				ImageSrc:         "https://img",
				WSPrice:          "100.00",
				FulfillmentType:  product.FulfillmentTypeShipReady,
				InventoryTracked: true,
			},
		}},
	}

	session := "sess"
	result, err := svc.ReconcileShipReadyAgainstInventory(context.Background(), nil, &session, map[string]int{
		"gid://v/1": 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Changed {
		t.Fatal("expected cart change")
	}
	if store.lastShip != 2 || store.lastPre != 5 {
		t.Fatalf("expected ship=2 pre=5, got ship=%d pre=%d", store.lastShip, store.lastPre)
	}
	if len(result.Variants) != 1 || result.Variants[0].MovedToPreorder != 3 {
		t.Fatalf("unexpected depletion details: %+v", result.Variants)
	}
}

func TestReconcileShipReadyAgainstInventory_NoChangeWhenStockSufficient(t *testing.T) {
	t.Parallel()

	store := &reconcileCartStore{
		cart: &Cart{
			ID: "cart-1",
			Items: []CartItemModel{
				{ShopifyVariantID: "gid://v/1", FulfillmentType: "ship_ready", Quantity: 2},
			},
		},
	}
	svc := &service{
		store: store,
		productService: &reconcileProductStub{variants: map[string]*product.VariantDTO{
			"gid://v/1": {
				ID:               "gid://v/1",
				Title:            "Stool",
				WSPrice:          "100.00",
				FulfillmentType:  product.FulfillmentTypeShipReady,
				InventoryTracked: true,
			},
		}},
	}

	session := "sess"
	result, err := svc.ReconcileShipReadyAgainstInventory(context.Background(), nil, &session, map[string]int{
		"gid://v/1": 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Changed {
		t.Fatal("expected no cart change")
	}
	if len(store.upserts) != 0 {
		t.Fatalf("expected no upserts, got %d", len(store.upserts))
	}
}
