package customer

import (
	"context"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type CustomerService interface {
	ListCustomers(ctx context.Context, query ListCustomersQuery) ([]CustomerResponse, int64, error)
	GetCustomer(ctx context.Context, id string) (*CustomerDetailResponse, error)
	GetCustomerOrders(ctx context.Context, customerID string) ([]CustomerOrderResponse, error)
}

type service struct {
	store CustomerStore
}

func NewCustomerService(store CustomerStore) CustomerService {
	return &service{store: store}
}

func (s *service) ListCustomers(ctx context.Context, query ListCustomersQuery) ([]CustomerResponse, int64, error) {
	if query.Page < 1 { query.Page = 1 }
	if query.Limit < 1 { query.Limit = 10 }
	
	customers, total, err := s.store.ListCustomers(ctx, query.Page, query.Limit, query.Search)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}

	var res []CustomerResponse
	for _, c := range customers {
		res = append(res, CustomerResponse{
			ID:          c.ID,
			Email:       c.Email,
			FirstName:   c.FirstName,
			LastName:    c.LastName,
			Phone:       c.Phone,
			OrdersCount: c.OrdersCount,
			CreatedAt:   c.CreatedAt,
		})
	}
	if res == nil { res = []CustomerResponse{} }
	return res, total, nil
}

func (s *service) GetCustomer(ctx context.Context, id string) (*CustomerDetailResponse, error) {
	c, err := s.store.GetCustomerByID(ctx, id)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if c == nil {
		return nil, apierror.ErrNotFound
	}

	var addresses []AddressResponse
	for _, a := range c.Addresses {
		addresses = append(addresses, AddressResponse{
			ID:        a.ID,
			FirstName: a.FirstName,
			LastName:  a.LastName,
			Address1:  a.Address1,
			Address2:  a.Address2,
			City:      a.City,
			Province:  a.Province,
			Country:   a.Country,
			Zip:       a.Zip,
			Phone:     a.Phone,
			IsDefault: a.IsDefault,
		})
	}
	if addresses == nil { addresses = []AddressResponse{} }

	res := &CustomerDetailResponse{
		CustomerResponse: CustomerResponse{
			ID:          c.ID,
			Email:       c.Email,
			FirstName:   c.FirstName,
			LastName:    c.LastName,
			Phone:       c.Phone,
			OrdersCount: c.OrdersCount,
			CreatedAt:   c.CreatedAt,
		},
		Addresses: addresses,
	}
	return res, nil
}

func (s *service) GetCustomerOrders(ctx context.Context, customerID string) ([]CustomerOrderResponse, error) {
	// First verify customer exists
	c, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil { return nil, apierror.ErrInternal }
	if c == nil { return nil, apierror.ErrNotFound }

	orders, err := s.store.GetOrdersByCustomer(ctx, customerID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	var res []CustomerOrderResponse
	for _, o := range orders {
		res = append(res, CustomerOrderResponse{
			ID:              o.ID,
			TotalPrice:      o.TotalPrice,
			AggregateStatus: o.AggregateStatus,
			CreatedAt:       o.CreatedAt,
		})
	}
	if res == nil { res = []CustomerOrderResponse{} }
	return res, nil
}
