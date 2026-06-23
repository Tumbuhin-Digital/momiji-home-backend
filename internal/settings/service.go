package settings

import (
	"context"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type SettingsService interface {
	GetCheckoutNotes(ctx context.Context) (*CheckoutNotesResponse, error)
	UpdateCheckoutNotes(ctx context.Context, req UpdateSettingsRequest) (*CheckoutNotesResponse, error)
}

type service struct {
	store SettingsStore
}

func NewSettingsService(store SettingsStore) SettingsService {
	return &service{store: store}
}

func (s *service) GetCheckoutNotes(ctx context.Context) (*CheckoutNotesResponse, error) {
	values, err := s.store.GetMany(ctx, []string{KeyCheckoutDueNowNote, KeyCheckoutDueLaterNote})
	if err != nil {
		return nil, apierror.ErrInternal
	}

	return &CheckoutNotesResponse{
		DueNowNote:   values[KeyCheckoutDueNowNote],
		DueLaterNote: values[KeyCheckoutDueLaterNote],
	}, nil
}

func (s *service) UpdateCheckoutNotes(ctx context.Context, req UpdateSettingsRequest) (*CheckoutNotesResponse, error) {
	dueNowNote := strings.TrimSpace(req.DueNowNote)
	dueLaterNote := strings.TrimSpace(req.DueLaterNote)

	if dueNowNote == "" || dueLaterNote == "" {
		return nil, apierror.NewWithDetails(
			apierror.ErrBadRequest.Status,
			apierror.ErrBadRequest.Code,
			"Both checkout notes are required",
			nil,
		)
	}

	if err := s.store.Upsert(ctx, KeyCheckoutDueNowNote, dueNowNote); err != nil {
		return nil, apierror.ErrInternal
	}
	if err := s.store.Upsert(ctx, KeyCheckoutDueLaterNote, dueLaterNote); err != nil {
		return nil, apierror.ErrInternal
	}

	return &CheckoutNotesResponse{
		DueNowNote:   dueNowNote,
		DueLaterNote: dueLaterNote,
	}, nil
}
