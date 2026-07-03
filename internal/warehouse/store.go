package warehouse

import "context"

type Store interface {
	List(ctx context.Context) ([]Warehouse, error)
	GetByCode(ctx context.Context, code string) (*Warehouse, error)
	UpdateByCode(ctx context.Context, code string, updates map[string]interface{}) (*Warehouse, error)
}
