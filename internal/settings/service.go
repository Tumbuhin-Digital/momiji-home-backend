package settings

import (
	"context"
	"strconv"
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
	values, err := s.store.GetMany(ctx, []string{
		KeyCheckoutDueNowNote,
		KeyCheckoutDueLaterNote,
		KeyCheckoutPreorderShippingNote,
		KeyStoreClosed,
		KeyStoreClosedMessage,
	})
	if err != nil {
		return nil, apierror.ErrInternal
	}

	storeClosed, err := strconv.ParseBool(strings.TrimSpace(values[KeyStoreClosed]))
	if err != nil {
		storeClosed = false
	}

	return &CheckoutNotesResponse{
		DueNowNote:           values[KeyCheckoutDueNowNote],
		DueLaterNote:         values[KeyCheckoutDueLaterNote],
		PreorderShippingNote: values[KeyCheckoutPreorderShippingNote],
		StoreClosed:          storeClosed,
		StoreClosedMessage:   values[KeyStoreClosedMessage],
	}, nil
}

func (s *service) UpdateCheckoutNotes(ctx context.Context, req UpdateSettingsRequest) (*CheckoutNotesResponse, error) {
	dueNowNote := strings.TrimSpace(req.DueNowNote)
	dueLaterNote := strings.TrimSpace(req.DueLaterNote)
	preorderShippingNote := strings.TrimSpace(req.PreorderShippingNote)
	storeClosedMessage := strings.TrimSpace(req.StoreClosedMessage)

	if dueNowNote == "" || dueLaterNote == "" || preorderShippingNote == "" {
		return nil, apierror.NewWithDetails(
			apierror.ErrBadRequest.Status,
			apierror.ErrBadRequest.Code,
			"All checkout notes are required",
			nil,
		)
	}

	if req.StoreClosed && storeClosedMessage == "" {
		return nil, apierror.NewWithDetails(
			apierror.ErrBadRequest.Status,
			apierror.ErrBadRequest.Code,
			"Store closed message is required when the store is closed",
			nil,
		)
	}

	if err := s.store.Upsert(ctx, KeyCheckoutDueNowNote, dueNowNote); err != nil {
		return nil, apierror.ErrInternal
	}
	if err := s.store.Upsert(ctx, KeyCheckoutDueLaterNote, dueLaterNote); err != nil {
		return nil, apierror.ErrInternal
	}
	if err := s.store.Upsert(ctx, KeyCheckoutPreorderShippingNote, preorderShippingNote); err != nil {
		return nil, apierror.ErrInternal
	}
	if err := s.store.Upsert(ctx, KeyStoreClosed, strconv.FormatBool(req.StoreClosed)); err != nil {
		return nil, apierror.ErrInternal
	}
	if err := s.store.Upsert(ctx, KeyStoreClosedMessage, storeClosedMessage); err != nil {
		return nil, apierror.ErrInternal
	}

	return &CheckoutNotesResponse{
		DueNowNote:           dueNowNote,
		DueLaterNote:         dueLaterNote,
		PreorderShippingNote: preorderShippingNote,
		StoreClosed:          req.StoreClosed,
		StoreClosedMessage:   storeClosedMessage,
	}, nil
}
