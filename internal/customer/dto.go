package customer

import "time"

type CustomerResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	FirstName   *string   `json:"first_name,omitempty"`
	LastName    *string   `json:"last_name,omitempty"`
	Phone       *string   `json:"phone,omitempty"`
	OrdersCount int       `json:"orders_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type AddressResponse struct {
	ID        string  `json:"id"`
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
	Address1  string  `json:"address1"`
	Address2  *string `json:"address2,omitempty"`
	City      string  `json:"city"`
	Province  string  `json:"province"`
	Country   string  `json:"country"`
	Zip       string  `json:"zip"`
	Phone     *string `json:"phone,omitempty"`
	IsDefault bool    `json:"is_default"`
}

type CustomerDetailResponse struct {
	CustomerResponse
	Addresses []AddressResponse `json:"addresses"`
}

type CustomerOrderResponse struct {
	ID              string    `json:"id"`
	TotalPrice      float64   `json:"total_price"`
	AggregateStatus string    `json:"aggregate_status"`
	CreatedAt       time.Time `json:"created_at"`
}

type ListCustomersQuery struct {
	Page   int    `query:"page"`
	Limit  int    `query:"limit"`
	Search string `query:"search"`
}
