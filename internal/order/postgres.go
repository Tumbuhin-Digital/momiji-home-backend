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

func (s *PostgresStore) GetOrdersByCustomer(ctx context.Context, customerID string) ([]Order, error) {
	var orders []Order
	err := s.db.WithContext(ctx).Preload("Items").Where("customer_id = ?", customerID).Find(&orders).Error
	return orders, err
}

func (s *PostgresStore) UpdateOrderStatus(ctx context.Context, orderID string, status string) error {
	return s.db.WithContext(ctx).Model(&Order{}).Where("id = ?", orderID).Update("aggregate_status", status).Error
}

func (s *PostgresStore) UpdateOrderItemStep(ctx context.Context, itemID string, step int) error {
	return s.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).Update("fulfillment_step", step).Error
}

func (s *PostgresStore) UpdateOrderItemReceived(ctx context.Context, itemID string, count int) error {
	return s.db.WithContext(ctx).Model(&OrderItem{}).Where("id = ?", itemID).Update("items_received", count).Error
}
