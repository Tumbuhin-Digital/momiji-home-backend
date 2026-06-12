package customer

import (
	"context"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) CustomerStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) ListCustomers(ctx context.Context, page, limit int, search string) ([]Customer, int64, error) {
	var customers []Customer
	var total int64

	query := s.db.WithContext(ctx).Model(&Customer{})
	
	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("email ILIKE ? OR first_name ILIKE ? OR last_name ILIKE ?", searchPattern, searchPattern, searchPattern)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&customers).Error; err != nil {
		return nil, 0, err
	}

	// Populate OrdersCount for each customer
	// In a real application, you might do this with a JOIN/GROUP BY in the main query for efficiency
	for i := range customers {
		var count int64
		s.db.WithContext(ctx).Table("orders").Where("customer_id = ?", customers[i].ID).Count(&count)
		customers[i].OrdersCount = int(count)
	}

	return customers, total, nil
}

func (s *PostgresStore) GetCustomerByID(ctx context.Context, id string) (*Customer, error) {
	var customer Customer
	err := s.db.WithContext(ctx).Preload("Addresses").Where("id = ?", id).First(&customer).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	
	var count int64
	s.db.WithContext(ctx).Table("orders").Where("customer_id = ?", customer.ID).Count(&count)
	customer.OrdersCount = int(count)
	
	return &customer, nil
}

func (s *PostgresStore) GetOrdersByCustomer(ctx context.Context, customerID string) ([]CustomerOrder, error) {
	var orders []CustomerOrder
	err := s.db.WithContext(ctx).Table("orders").
		Select("id, total_price, aggregate_status, created_at").
		Where("customer_id = ?", customerID).
		Order("created_at DESC").
		Scan(&orders).Error
	if err != nil {
		return nil, err
	}
	return orders, nil
}

func (s *PostgresStore) UpsertCustomer(ctx context.Context, cust *Customer) error {
	return s.db.WithContext(ctx).Save(cust).Error
}
