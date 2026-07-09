package preordercustomtext

import (
	"context"
	"errors"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"gorm.io/gorm"
)

type Service interface {
	List(ctx context.Context, search string) ([]PreorderCustomTextDTO, error)
	Create(ctx context.Context, label string) (*PreorderCustomTextDTO, error)
	Delete(ctx context.Context, id string) (*DeletePreorderCustomTextResponse, error)
	EnsureByLabel(ctx context.Context, label string) (*PreorderCustomText, error)
}

type service struct {
	store Store
}

func NewService(store Store) Service {
	return &service{store: store}
}

func (s *service) List(ctx context.Context, search string) ([]PreorderCustomTextDTO, error) {
	items, err := s.store.List(ctx, search)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	dtos := make([]PreorderCustomTextDTO, 0, len(items))
	for _, item := range items {
		usage, err := s.store.CountVariantUsage(ctx, item.Label)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		dtos = append(dtos, PreorderCustomTextDTO{
			ID:         item.ID,
			Label:      item.Label,
			UsageCount: usage,
		})
	}
	return dtos, nil
}

func (s *service) Create(ctx context.Context, label string) (*PreorderCustomTextDTO, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil, apierror.New(400, "validation_error", "label is required")
	}
	if len(label) > 128 {
		return nil, apierror.New(400, "validation_error", "label must be at most 128 characters")
	}

	existing, err := s.store.GetByLabel(ctx, label)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if existing != nil {
		return nil, apierror.New(409, "duplicate_label", "A custom text with this label already exists")
	}

	item, err := s.store.Create(ctx, label)
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, apierror.New(409, "duplicate_label", "A custom text with this label already exists")
		}
		return nil, apierror.ErrInternal
	}

	return &PreorderCustomTextDTO{
		ID:         item.ID,
		Label:      item.Label,
		UsageCount: 0,
	}, nil
}

func (s *service) Delete(ctx context.Context, id string) (*DeletePreorderCustomTextResponse, error) {
	item, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if item == nil {
		return nil, apierror.ErrNotFound
	}

	usage, err := s.store.CountVariantUsage(ctx, item.Label)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	if err := s.store.SoftDelete(ctx, id); err != nil {
		return nil, apierror.ErrInternal
	}

	return &DeletePreorderCustomTextResponse{
		ID:         item.ID,
		Label:      item.Label,
		UsageCount: usage,
	}, nil
}

func (s *service) EnsureByLabel(ctx context.Context, label string) (*PreorderCustomText, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil, nil
	}
	return s.store.EnsureByLabel(ctx, label)
}
