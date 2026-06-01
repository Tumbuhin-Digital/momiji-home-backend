package order

import (
	"context"

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

func (s *PostgresStore) GetOrder(ctx context.Context, orderID, customerID string) (*Order, error) {
	var order Order
	err := s.db.WithContext(ctx).Preload("Items").Where("id = ? AND customer_id = ?", orderID, customerID).First(&order).Error
	if err != nil {
		return nil, err
	}
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

	err := query.Preload("Items").Order("created_at DESC").Offset(offset).Limit(limit).Find(&orders).Error
	return orders, total, err
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

func (s *PostgresStore) UpdateOrderItemReceived(ctx context.Context, itemID string, count int) error {
	return s.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).Update("items_received", count).Error
}
