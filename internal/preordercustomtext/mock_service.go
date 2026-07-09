package preordercustomtext

import "context"

// MockService is an in-memory Service for unit tests.
type MockService struct {
	ListFn          func(ctx context.Context, search string) ([]PreorderCustomTextDTO, error)
	CreateFn        func(ctx context.Context, label string) (*PreorderCustomTextDTO, error)
	DeleteFn        func(ctx context.Context, id string) (*DeletePreorderCustomTextResponse, error)
	EnsureByLabelFn func(ctx context.Context, label string) (*PreorderCustomText, error)
}

func NewMockService() *MockService {
	return &MockService{}
}

func (m *MockService) List(ctx context.Context, search string) ([]PreorderCustomTextDTO, error) {
	if m.ListFn != nil {
		return m.ListFn(ctx, search)
	}
	return nil, nil
}

func (m *MockService) Create(ctx context.Context, label string) (*PreorderCustomTextDTO, error) {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, label)
	}
	return &PreorderCustomTextDTO{ID: "mock-id", Label: label}, nil
}

func (m *MockService) Delete(ctx context.Context, id string) (*DeletePreorderCustomTextResponse, error) {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id)
	}
	return &DeletePreorderCustomTextResponse{ID: id}, nil
}

func (m *MockService) EnsureByLabel(ctx context.Context, label string) (*PreorderCustomText, error) {
	if m.EnsureByLabelFn != nil {
		return m.EnsureByLabelFn(ctx, label)
	}
	return &PreorderCustomText{ID: "mock-id", Label: label}, nil
}
