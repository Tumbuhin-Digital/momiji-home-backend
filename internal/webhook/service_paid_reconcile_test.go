package webhook

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorderbatch"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"gorm.io/gorm"
)

type reconcileOrderStore struct {
	fulfillmentTestStore
	created            *order.Order
	createErr          error
	getByShopify       *order.Order
	getByShopifyErr    error
	existingShipment   *order.PreorderShipment
	getShipmentErr     error
	upsertShipmentCalls int
	upsertShipmentErr  error
	dims               map[string]order.VariantDimensions
	dimsErr            error
}

func (s *reconcileOrderStore) GetOrderByShopifyID(context.Context, string) (*order.Order, error) {
	if s.getByShopifyErr != nil {
		return nil, s.getByShopifyErr
	}
	return s.getByShopify, nil
}

func (s *reconcileOrderStore) CreateOrder(_ context.Context, o *order.Order) error {
	if s.createErr != nil {
		return s.createErr
	}
	if o.ID == "" {
		o.ID = "order-created"
	}
	for i := range o.Items {
		if o.Items[i].ID == "" {
			o.Items[i].ID = fmt.Sprintf("line-%d", i+1)
		}
	}
	copied := *o
	copied.Items = append([]order.OrderItem(nil), o.Items...)
	s.created = &copied
	s.getByShopify = &copied
	return nil
}

func (s *reconcileOrderStore) GetPreorderShipment(context.Context, string) (*order.PreorderShipment, error) {
	if s.getShipmentErr != nil {
		return nil, s.getShipmentErr
	}
	return s.existingShipment, nil
}

func (s *reconcileOrderStore) UpsertPreorderShipment(_ context.Context, shipment *order.PreorderShipment, _ []order.PreorderPackingItem) error {
	s.upsertShipmentCalls++
	if s.upsertShipmentErr != nil {
		return s.upsertShipmentErr
	}
	s.existingShipment = shipment
	return nil
}

func (s *reconcileOrderStore) GetVariantDimensions(_ context.Context, variantIDs []string) (map[string]order.VariantDimensions, error) {
	if s.dimsErr != nil {
		return nil, s.dimsErr
	}
	if s.dims != nil {
		return s.dims, nil
	}
	out := make(map[string]order.VariantDimensions, len(variantIDs))
	for _, id := range variantIDs {
		out[id] = order.VariantDimensions{
			ShopifyVariantID: id,
			SKU:              "SKU-1",
			WeightKg:         1,
			WidthCm:          10,
			HeightCm:         8,
			DepthCm:          4,
		}
	}
	return out, nil
}

type reconcilePreorderStore struct {
	settlements map[string]*preorder.Settlement
	createCalls int
	createErr   error
	lookupErr   error
}

func (s *reconcilePreorderStore) CreateSettlement(_ context.Context, settlement *preorder.Settlement) error {
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	if s.settlements == nil {
		s.settlements = map[string]*preorder.Settlement{}
	}
	copied := *settlement
	if copied.ID == "" {
		copied.ID = fmt.Sprintf("st-%d", s.createCalls)
	}
	s.settlements[settlement.OrderLineItemID] = &copied
	return nil
}

func (s *reconcilePreorderStore) GetSettlementByID(context.Context, string) (*preorder.Settlement, error) {
	return nil, nil
}

func (s *reconcilePreorderStore) GetSettlementByOrderLineItemID(_ context.Context, itemID string) (*preorder.Settlement, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	if s.settlements == nil {
		return nil, gorm.ErrRecordNotFound
	}
	st, ok := s.settlements[itemID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return st, nil
}

func (s *reconcilePreorderStore) ListSettlements(context.Context, preorder.SettlementFilter) ([]preorder.PreorderRow, int64, error) {
	return nil, 0, nil
}
func (s *reconcilePreorderStore) GetAllSettlementsForExport(context.Context, preorder.SettlementFilter) ([]preorder.PreorderRow, error) {
	return nil, nil
}
func (s *reconcilePreorderStore) UpdateSettlementStatus(context.Context, string, string, *time.Time) error {
	return nil
}
func (s *reconcilePreorderStore) MarkSettlementsInvoiced(context.Context, []string, string, string, time.Time) error {
	return nil
}
func (s *reconcilePreorderStore) AllSettlementsPaid(context.Context, string) (bool, error) {
	return false, nil
}
func (s *reconcilePreorderStore) GetSettlementsForReminder(context.Context, int) ([]preorder.PreorderRow, error) {
	return nil, nil
}

var _ preorder.PreorderStore = (*reconcilePreorderStore)(nil)

type stubBatchAllocator struct {
	calls   int
	failUntil int
	err     error
}

func (a *stubBatchAllocator) AllocateToBatch(context.Context, string, int, preorderbatch.AllocationRef) (*preorderbatch.AllocateResult, error) {
	a.calls++
	if a.failUntil > 0 && a.calls <= a.failUntil {
		if a.err != nil {
			return nil, a.err
		}
		return nil, errors.New("allocate failed")
	}
	return &preorderbatch.AllocateResult{}, nil
}

func (a *stubBatchAllocator) GetCommittedAllocationsByOrderLineItemIDs(context.Context, []string) ([]preorderbatch.BatchAllocation, error) {
	return nil, nil
}

func (a *stubBatchAllocator) GetBatchesByIDs(context.Context, []string) ([]preorderbatch.PreorderBatch, error) {
	return nil, nil
}

type stubCustomerStore struct{}

func (stubCustomerStore) ListCustomers(context.Context, int, int, string) ([]customer.Customer, int64, error) {
	return nil, 0, nil
}
func (stubCustomerStore) GetCustomerByID(context.Context, string) (*customer.Customer, error) {
	return nil, nil
}
func (stubCustomerStore) GetOrdersByCustomer(context.Context, string) ([]customer.CustomerOrder, error) {
	return nil, nil
}
func (stubCustomerStore) UpsertCustomer(context.Context, *customer.Customer) error { return nil }
func (stubCustomerStore) CreateAddress(_ context.Context, addr *customer.Address) error {
	if addr.ID == "" {
		addr.ID = "addr-1"
	}
	return nil
}

func wholesalePreorderPayload() ShopifyOrderWebhook {
	return ShopifyOrderWebhook{
		ID:          9001,
		OrderNumber: 42,
		Email:       "buyer@example.com",
		Currency:    "USD",
		Customer: ShopifyCustomer{
			ID:        1,
			Email:     "buyer@example.com",
			FirstName: "Buyer",
		},
		NoteAttributes: []ShopifyProperty{
			{Name: "source", Value: "wholesale"},
			{Name: "checkout_reference", Value: "checkout-ref-1"},
			{Name: "preorder_shipping_estimate", Value: "12.50"},
			{Name: "preorder_warehouse_origin", Value: "west"},
		},
		LineItems: []ShopifyOrderLineItem{
			{
				VariantID: 111,
				Title:     "Preorder Hoodie",
				Quantity:  2,
				Price:     "25.00",
				Properties: []ShopifyProperty{
					{Name: "type", Value: "preorder_dp"},
					{Name: "full_price", Value: "50"},
					{Name: "variant_ref", Value: "gid://shopify/ProductVariant/111"},
				},
			},
		},
	}
}

func TestFinalizePaidCheckoutOrder_SettlementCreateFailureReturnsError(t *testing.T) {
	balance := 50.0
	shopifyOrderID := "9001"
	store := &reconcileOrderStore{}
	preorderStore := &reconcilePreorderStore{
		createErr: errors.New("settlement insert failed"),
	}
	batch := &stubBatchAllocator{}

	svc := &service{
		orderStore:    store,
		preorderStore: preorderStore,
		batchService:  batch,
	}

	err := svc.finalizePaidCheckoutOrder(context.Background(), &order.Order{
		ID:             "order-1",
		CustomerID:     "cust-1",
		ShopifyOrderID: &shopifyOrderID,
		Items: []order.OrderItem{
			{
				ID:               "line-1",
				Type:             "pre_order",
				ShopifyVariantID: "gid://shopify/ProductVariant/111",
				Quantity:         2,
				BalanceDue:       &balance,
			},
		},
	}, paidCheckoutMeta{CheckoutRef: "checkout-ref-1"})
	if err == nil {
		t.Fatal("expected settlement create failure to return error")
	}
	if preorderStore.createCalls != 1 {
		t.Fatalf("expected 1 settlement create attempt, got %d", preorderStore.createCalls)
	}
}

func TestFinalizePaidCheckoutOrder_SkipsExistingSettlementAndShipment(t *testing.T) {
	balance := 50.0
	shopifyOrderID := "9001"
	store := &reconcileOrderStore{
		existingShipment: &order.PreorderShipment{ID: "ship-1", OrderID: "order-1"},
	}
	preorderStore := &reconcilePreorderStore{
		settlements: map[string]*preorder.Settlement{
			"line-1": {ID: "st-1", OrderLineItemID: "line-1", Status: "pending"},
		},
	}
	batch := &stubBatchAllocator{}

	svc := &service{
		orderStore:    store,
		preorderStore: preorderStore,
		batchService:  batch,
	}

	err := svc.finalizePaidCheckoutOrder(context.Background(), &order.Order{
		ID:             "order-1",
		CustomerID:     "cust-1",
		ShopifyOrderID: &shopifyOrderID,
		Items: []order.OrderItem{
			{
				ID:               "line-1",
				Type:             "pre_order",
				ShopifyVariantID: "gid://shopify/ProductVariant/111",
				Quantity:         2,
				BalanceDue:       &balance,
			},
		},
	}, paidCheckoutMeta{CheckoutRef: "checkout-ref-1"})
	if err != nil {
		t.Fatalf("finalizePaidCheckoutOrder: %v", err)
	}
	if preorderStore.createCalls != 0 {
		t.Fatalf("expected no settlement create on complete retry, got %d", preorderStore.createCalls)
	}
	if store.upsertShipmentCalls != 0 {
		t.Fatalf("expected no shipment upsert when shipment exists, got %d", store.upsertShipmentCalls)
	}
	if batch.calls != 1 {
		t.Fatalf("expected allocate still called (idempotent), got %d", batch.calls)
	}
}

func TestHandleOrderPaid_RetryAfterAllocateFailureCompletesSideEffects(t *testing.T) {
	productStore := product.NewMockProductStore()
	productStore.Variants["gid://shopify/ProductVariant/111"] = &product.ProductVariant{
		ID:               "variant-uuid-1",
		ShopifyVariantID: "gid://shopify/ProductVariant/111",
	}

	store := &reconcileOrderStore{}
	preorderStore := &reconcilePreorderStore{}
	batch := &stubBatchAllocator{failUntil: 1}
	authStore := auth.NewMockAuthStore()

	svc := &service{
		orderStore:    store,
		authStore:     authStore,
		productStore:  productStore,
		preorderStore: preorderStore,
		customerStore: stubCustomerStore{},
		batchService:  batch,
	}

	payload := wholesalePreorderPayload()

	if err := svc.HandleOrderPaid(context.Background(), payload); err == nil {
		t.Fatal("expected first attempt to fail on allocate")
	}
	if store.created == nil {
		t.Fatal("expected order to be created before allocate failure")
	}
	if preorderStore.createCalls != 0 {
		t.Fatalf("settlement must not be created before allocate succeeds, got %d", preorderStore.createCalls)
	}
	if store.upsertShipmentCalls != 0 {
		t.Fatalf("shipment must not be created before allocate succeeds, got %d", store.upsertShipmentCalls)
	}

	// Retry: order already exists → reconcile remaining side effects.
	if err := svc.HandleOrderPaid(context.Background(), payload); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if batch.calls != 2 {
		t.Fatalf("expected allocate called twice (fail then success), got %d", batch.calls)
	}
	if preorderStore.createCalls != 1 {
		t.Fatalf("expected settlement created on retry, got %d creates", preorderStore.createCalls)
	}
	if store.upsertShipmentCalls != 1 {
		t.Fatalf("expected shipment created on retry, got %d upserts", store.upsertShipmentCalls)
	}
}

func TestHandleOrderPaid_FullyCompleteRetryIsIdempotent(t *testing.T) {
	productStore := product.NewMockProductStore()
	productStore.Variants["gid://shopify/ProductVariant/111"] = &product.ProductVariant{
		ID:               "variant-uuid-1",
		ShopifyVariantID: "gid://shopify/ProductVariant/111",
	}

	store := &reconcileOrderStore{}
	preorderStore := &reconcilePreorderStore{}
	batch := &stubBatchAllocator{}
	authStore := auth.NewMockAuthStore()

	svc := &service{
		orderStore:    store,
		authStore:     authStore,
		productStore:  productStore,
		preorderStore: preorderStore,
		customerStore: stubCustomerStore{},
		batchService:  batch,
	}

	payload := wholesalePreorderPayload()
	if err := svc.HandleOrderPaid(context.Background(), payload); err != nil {
		t.Fatalf("first paid webhook: %v", err)
	}
	if preorderStore.createCalls != 1 {
		t.Fatalf("expected 1 settlement create, got %d", preorderStore.createCalls)
	}
	if store.upsertShipmentCalls != 1 {
		t.Fatalf("expected 1 shipment upsert, got %d", store.upsertShipmentCalls)
	}

	if err := svc.HandleOrderPaid(context.Background(), payload); err != nil {
		t.Fatalf("complete retry: %v", err)
	}
	if preorderStore.createCalls != 1 {
		t.Fatalf("expected no duplicate settlement, got %d creates", preorderStore.createCalls)
	}
	if store.upsertShipmentCalls != 1 {
		t.Fatalf("expected no duplicate shipment, got %d upserts", store.upsertShipmentCalls)
	}
	if batch.calls != 2 {
		t.Fatalf("expected allocate on both attempts, got %d", batch.calls)
	}
}
