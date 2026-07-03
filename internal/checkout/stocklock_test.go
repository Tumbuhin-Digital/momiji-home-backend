package checkout

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type memoryStockLockStore struct {
	mu    sync.Mutex
	locks []StockLock
}

func (m *memoryStockLockStore) GetActiveLocksForVariant(_ context.Context, shopifyVariantID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	total := 0
	for _, lock := range m.locks {
		if lock.ShopifyVariantID == shopifyVariantID && lock.ExpiresAt.After(now) {
			total += lock.Quantity
		}
	}
	return total, nil
}

func (m *memoryStockLockStore) CreateLocks(_ context.Context, locks []StockLock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locks = append(m.locks, locks...)
	return nil
}

func (m *memoryStockLockStore) DeleteLocksBySession(_ context.Context, userID, sessionID *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := m.locks[:0]
	for _, lock := range m.locks {
		if matchesIdentity(lock.UserID, lock.SessionID, userID, sessionID) {
			continue
		}
		filtered = append(filtered, lock)
	}
	m.locks = filtered
	return nil
}

func (m *memoryStockLockStore) DeleteLocksByCheckoutReference(
	_ context.Context,
	checkoutReference string,
	userID, sessionID *string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := m.locks[:0]
	for _, lock := range m.locks {
		if lock.CheckoutReference != nil && *lock.CheckoutReference == checkoutReference {
			if userID == nil && sessionID == nil {
				continue
			}
			if matchesIdentity(lock.UserID, lock.SessionID, userID, sessionID) {
				continue
			}
		}
		filtered = append(filtered, lock)
	}
	m.locks = filtered
	return nil
}

func (m *memoryStockLockStore) DeleteExpiredLocks(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	filtered := m.locks[:0]
	for _, lock := range m.locks {
		if lock.ExpiresAt.After(now) {
			filtered = append(filtered, lock)
		}
	}
	m.locks = filtered
	return nil
}

func (m *memoryStockLockStore) GetUSZipCodeDetails(context.Context, string) (*UsZipCode, error) {
	return nil, nil
}

func (m *memoryStockLockStore) countLocks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.locks)
}

func (m *memoryStockLockStore) lockedQtyForVariant(variantID string) int {
	qty, _ := m.GetActiveLocksForVariant(context.Background(), variantID)
	return qty
}

func matchesIdentity(lockUserID, lockSessionID, userID, sessionID *string) bool {
	if userID != nil && *userID != "" && lockUserID != nil && *lockUserID == *userID {
		return true
	}
	if sessionID != nil && *sessionID != "" && lockSessionID != nil && *lockSessionID == *sessionID {
		return true
	}
	return false
}

type mockShopifyInventoryClient struct {
	inventory map[string]int
}

func (m *mockShopifyInventoryClient) QueryAdminGraphQL(context.Context, string, map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (m *mockShopifyInventoryClient) CreateDraftOrder(context.Context, shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return nil, nil
}

func (m *mockShopifyInventoryClient) CreateStorefrontCart(context.Context, shopify.CartCreateInput) (*shopify.CartCreateResponse, error) {
	return nil, nil
}

func (m *mockShopifyInventoryClient) CreateRefund(context.Context, string, float64, string, string) error {
	return nil
}

func (m *mockShopifyInventoryClient) CreateFulfillment(context.Context, string) error {
	return nil
}

func (m *mockShopifyInventoryClient) FetchFulfillmentOrders(context.Context, string) ([]shopify.FulfillmentOrderData, error) {
	return nil, nil
}

func (m *mockShopifyInventoryClient) CreateFulfillmentV2(context.Context, shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	return nil, nil
}

func (m *mockShopifyInventoryClient) CreateFulfillmentEvent(context.Context, string, string) error {
	return nil
}

func (m *mockShopifyInventoryClient) GetVariantsInventory(_ context.Context, variantIDs []string) (map[string]int, error) {
	result := make(map[string]int, len(variantIDs))
	for _, id := range variantIDs {
		result[id] = m.inventory[id]
	}
	return result, nil
}

func newTestStockLockService(store StockLockStore, inventory map[string]int) StockLockService {
	return &stockLockService{
		store:      store,
		shopifyCli: &mockShopifyInventoryClient{inventory: inventory},
	}
}

func TestAcquireLocks_CreatesLocksAndReturnsExpiry(t *testing.T) {
	store := &memoryStockLockStore{}
	svc := newTestStockLockService(store, map[string]int{"variant-1": 10})

	sessionID := "sess_a"
	expiresAt, err := svc.AcquireLocks(context.Background(), nil, &sessionID, "ref-1", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expiresAt.IsZero() {
		t.Fatal("expected non-zero expiresAt")
	}
	if store.countLocks() != 1 {
		t.Fatalf("expected 1 lock row, got %d", store.countLocks())
	}
	if store.lockedQtyForVariant("variant-1") != 2 {
		t.Fatalf("expected locked qty 2, got %d", store.lockedQtyForVariant("variant-1"))
	}
}

func TestAcquireLocks_ReplacesExistingSessionLocks(t *testing.T) {
	store := &memoryStockLockStore{}
	svc := newTestStockLockService(store, map[string]int{"variant-1": 10})
	sessionID := "sess_a"

	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionID, "ref-1", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 2},
	}); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionID, "ref-2", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 3},
	}); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	if store.countLocks() != 1 {
		t.Fatalf("expected 1 lock row after replace, got %d", store.countLocks())
	}
	if store.lockedQtyForVariant("variant-1") != 3 {
		t.Fatalf("expected locked qty 3, got %d", store.lockedQtyForVariant("variant-1"))
	}
}

func TestAcquireLocks_OutOfStockWhenOtherSessionsHoldInventory(t *testing.T) {
	store := &memoryStockLockStore{}
	svc := newTestStockLockService(store, map[string]int{"variant-1": 5})

	sessionA := "sess_a"
	sessionB := "sess_b"
	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionA, "ref-a", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 4},
	}); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	_, err := svc.AcquireLocks(context.Background(), nil, &sessionB, "ref-b", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 2},
	})
	if err == nil {
		t.Fatal("expected out_of_stock error")
	}
	apiErr, ok := err.(*apierror.AppError)
	if !ok || apiErr.Code != "out_of_stock" {
		t.Fatalf("expected out_of_stock API error, got %v", err)
	}
}

func TestReleaseLocksByCheckoutReference_RemovesOnlyMatchingReference(t *testing.T) {
	store := &memoryStockLockStore{}
	svc := newTestStockLockService(store, map[string]int{"variant-1": 10, "variant-2": 10})

	sessionA := "sess_a"
	sessionB := "sess_b"
	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionA, "ref-a", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 1},
	}); err != nil {
		t.Fatalf("acquire A failed: %v", err)
	}
	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionB, "ref-b", []LockRequest{
		{ShopifyVariantID: "variant-2", Quantity: 1},
	}); err != nil {
		t.Fatalf("acquire B failed: %v", err)
	}

	if err := svc.ReleaseLocksByCheckoutReference(context.Background(), "ref-a"); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	if store.lockedQtyForVariant("variant-1") != 0 {
		t.Fatalf("expected variant-1 unlocked, got %d", store.lockedQtyForVariant("variant-1"))
	}
	if store.lockedQtyForVariant("variant-2") != 1 {
		t.Fatalf("expected variant-2 still locked, got %d", store.lockedQtyForVariant("variant-2"))
	}
}

func TestDeleteLocksBySession_DoesNotDeleteOtherUsersLocks(t *testing.T) {
	store := &memoryStockLockStore{}
	svc := newTestStockLockService(store, map[string]int{"variant-1": 10})

	sessionA := "sess_a"
	sessionB := "sess_b"
	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionA, "ref-a", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 1},
	}); err != nil {
		t.Fatalf("acquire A failed: %v", err)
	}
	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionB, "ref-b", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 2},
	}); err != nil {
		t.Fatalf("acquire B failed: %v", err)
	}

	if err := svc.ReleaseLocks(context.Background(), nil, &sessionA); err != nil {
		t.Fatalf("release A failed: %v", err)
	}

	if store.lockedQtyForVariant("variant-1") != 2 {
		t.Fatalf("expected session B lock to remain with qty 2, got %d", store.lockedQtyForVariant("variant-1"))
	}
}

func TestReleaseLocksForIdentity_ValidatesCheckoutReferenceOwnership(t *testing.T) {
	store := &memoryStockLockStore{}
	svc := newTestStockLockService(store, map[string]int{"variant-1": 10})

	sessionA := "sess_a"
	sessionB := "sess_b"
	if _, err := svc.AcquireLocks(context.Background(), nil, &sessionA, "ref-a", []LockRequest{
		{ShopifyVariantID: "variant-1", Quantity: 1},
	}); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	ref := "ref-a"
	if err := svc.ReleaseLocksForIdentity(context.Background(), nil, &sessionB, &ref); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	if store.lockedQtyForVariant("variant-1") != 1 {
		t.Fatalf("expected lock to remain for other session, got %d", store.lockedQtyForVariant("variant-1"))
	}

	if err := svc.ReleaseLocksForIdentity(context.Background(), nil, &sessionA, &ref); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if store.lockedQtyForVariant("variant-1") != 0 {
		t.Fatalf("expected lock released for owning session, got %d", store.lockedQtyForVariant("variant-1"))
	}
}
