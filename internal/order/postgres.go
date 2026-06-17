package order

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) CreateOrder(ctx context.Context, order *Order) error {
	return s.db.WithContext(ctx).Create(order).Error
}

func (s *PostgresStore) fillItemImages(ctx context.Context, items []*OrderItem) {
	if len(items) == 0 {
		return
	}
	var variantIDs []string
	for _, it := range items {
		variantIDs = append(variantIDs, it.ShopifyVariantID)
	}
	var variants []struct {
		ShopifyVariantID string
		ImageSrc         string
	}
	if err := s.db.WithContext(ctx).Table("product_variants").
		Select("shopify_variant_id, image_src").
		Where("shopify_variant_id IN ?", variantIDs).
		Find(&variants).Error; err == nil {
		variantMap := make(map[string]string)
		for _, v := range variants {
			variantMap[v.ShopifyVariantID] = v.ImageSrc
		}
		for i := range items {
			items[i].ImageSrc = variantMap[items[i].ShopifyVariantID]
		}
	}
}

func (s *PostgresStore) GetOrder(ctx context.Context, orderID, customerID string) (*Order, error) {
	var order Order
	query := s.db.WithContext(ctx).Preload("Items").Preload("Customer").Preload("ShippingAddress").Where("id = ?", orderID)
	if customerID != "" {
		query = query.Where("customer_id = ?", customerID)
	}
	err := query.First(&order).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var ptrItems []*OrderItem
	for i := range order.Items {
		ptrItems = append(ptrItems, &order.Items[i])
	}
	s.fillItemImages(ctx, ptrItems)

	return &order, nil
}

func (s *PostgresStore) GetOrderByShopifyID(ctx context.Context, shopifyOrderID string) (*Order, error) {
	var order Order
	err := s.db.WithContext(ctx).
		Preload("Items").
		Preload("Customer").
		Preload("ShippingAddress").
		Where("shopify_order_id = ? OR shopify_draft_order_id = ?", shopifyOrderID, shopifyOrderID).
		First(&order).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var ptrItems []*OrderItem
	for i := range order.Items {
		ptrItems = append(ptrItems, &order.Items[i])
	}
	s.fillItemImages(ctx, ptrItems)

	return &order, nil
}

func (s *PostgresStore) GetOrdersByCustomer(ctx context.Context, customerID string, q OrderQuery) ([]Order, int64, error) {
	var orders []Order
	var total int64

	query := s.db.WithContext(ctx).Model(&Order{})
	
	if customerID != "" {
		query = query.Where("customer_id = ?", customerID)
	}

	if q.Status != "" {
		// we match against aggregate_status since that's what might be passed as 'status', or financial_status.
		query = query.Where("aggregate_status = ? OR financial_status = ? OR fulfillment_status = ?", q.Status, q.Status, q.Status)
	}

	if q.Search != "" {
		searchPattern := "%" + q.Search + "%"
		query = query.Joins("JOIN users ON users.id = orders.customer_id").
			Where("orders.order_number ILIKE ? OR users.email ILIKE ?", searchPattern, searchPattern)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 { page = 1 }
	limit := q.Limit
	if limit < 1 { limit = 20 }
	offset := (page - 1) * limit

	err := query.Preload("Items").Preload("Customer").Preload("ShippingAddress").Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error
	if err != nil {
		return nil, 0, err
	}

	var ptrItems []*OrderItem
	for i := range orders {
		for j := range orders[i].Items {
			ptrItems = append(ptrItems, &orders[i].Items[j])
		}
	}
	s.fillItemImages(ctx, ptrItems)

	return orders, total, nil
}

func (s *PostgresStore) GetAllOrdersForExport(ctx context.Context, q OrderQuery) ([]Order, error) {
	var orders []Order
	query := s.db.WithContext(ctx).Model(&Order{})

	if q.Status != "" {
		query = query.Where("aggregate_status = ? OR financial_status = ? OR fulfillment_status = ?", q.Status, q.Status, q.Status)
	}

	if q.Search != "" {
		searchPattern := "%" + q.Search + "%"
		query = query.Joins("JOIN users ON users.id = orders.customer_id").
			Where("orders.order_number ILIKE ? OR users.email ILIKE ?", searchPattern, searchPattern)
	}

	err := query.Preload("Items").Preload("Customer").Preload("ShippingAddress").Order("created_at DESC").Find(&orders).Error
	if err != nil {
		return nil, err
	}

	var ptrItems []*OrderItem
	for i := range orders {
		for j := range orders[i].Items {
			ptrItems = append(ptrItems, &orders[i].Items[j])
		}
	}
	s.fillItemImages(ctx, ptrItems)

	return orders, nil
}

func (s *PostgresStore) UpdateOrderStatus(ctx context.Context, orderID, financialStatus, fulfillmentStatus string) error {
	return s.db.WithContext(ctx).Model(&Order{}).Where("id = ?", orderID).Updates(map[string]interface{}{
		"financial_status": financialStatus,
		"fulfillment_status": fulfillmentStatus,
	}).Error
}

func (s *PostgresStore) UpdateOrderItemStep(ctx context.Context, itemID string, step int) error {
	return s.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).Update("fulfillment_step", step).Error
}

func (s *PostgresStore) UpdateOrderItemTracking(ctx context.Context, itemID, trackingNumber, trackingURL string, shippedAt *time.Time) error {
	return s.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).Updates(map[string]interface{}{
		"tracking_number": trackingNumber,
		"tracking_url":    trackingURL,
		"shipped_at":      shippedAt,
		"item_status":     "shipped",
		"fulfillment_step": 3,
	}).Error
}

func (s *PostgresStore) UpdateOrderItemReceived(ctx context.Context, itemID string, count int) error {
	return s.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).Update("items_received", count).Error
}
