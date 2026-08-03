package customer

import (
	"context"
	"time"
)

type Customer struct {
	ID                string    `gorm:"primaryKey;type:uuid"`
	Email             string
	FirstName         *string
	LastName          *string
	Phone             *string
	ShopifyCustomerID *string
	CreatedAt         time.Time
	UpdatedAt         time.Time

	Addresses   []Address `gorm:"foreignKey:CustomerID"`
	OrdersCount int       `gorm:"-"`
}

type Address struct {
	ID         string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	CustomerID string
	FirstName  *string
	LastName   *string
	Company    *string
	Address1   string
	Address2   *string
	City       string
	Province   string
	Country    string
	Zip        string
	Phone      *string
	IsDefault  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (Address) TableName() string {
	return "customer_addresses"
}

type CustomerOrder struct {
	ID              string
	TotalPrice      float64
	AggregateStatus string
	CreatedAt       time.Time
}

type CustomerStore interface {
	ListCustomers(ctx context.Context, page, limit int, search string) ([]Customer, int64, error)
	GetCustomerByID(ctx context.Context, id string) (*Customer, error)
	GetOrdersByCustomer(ctx context.Context, customerID string) ([]CustomerOrder, error)
	UpsertCustomer(ctx context.Context, cust *Customer) error
	CreateAddress(ctx context.Context, addr *Address) error
}
