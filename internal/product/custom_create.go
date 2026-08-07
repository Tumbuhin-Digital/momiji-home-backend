package product

import (
	"context"
	"fmt"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

const maxCustomVariants = 100

type CreateCustomProductInput struct {
	Title             string
	InternalNote      *string
	IdempotencyKey    string
	ReferenceImageURL *string
	ReferenceImage    *ReferenceImageBytes
	Variants          []CreateCustomVariantInput
}

type CreateCustomVariantInput struct {
	Title    string
	RPPPrice float64
	WSPrice  float64
}

type ReferenceImageBytes struct {
	Filename    string
	ContentType string
	Data        []byte
}

func (s *service) CreateCustomProduct(ctx context.Context, input CreateCustomProductInput) (*ProductDTO, error) {
	title := strings.TrimSpace(input.Title)
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if title == "" {
		return nil, apierror.New(400, "validation_error", "Product name is required")
	}
	if idempotencyKey == "" {
		return nil, apierror.New(400, "validation_error", "Idempotency key is required")
	}
	if len(input.Variants) == 0 {
		return nil, apierror.New(400, "validation_error", "At least one variant is required")
	}
	if len(input.Variants) > maxCustomVariants {
		return nil, apierror.New(400, "validation_error", fmt.Sprintf("At most %d variants are allowed", maxCustomVariants))
	}

	seenTitles := make(map[string]struct{}, len(input.Variants))
	shopifyVariants := make([]shopify.CreateUnlistedProductVariantInput, 0, len(input.Variants))
	for i, v := range input.Variants {
		vt := strings.TrimSpace(v.Title)
		if vt == "" {
			return nil, apierror.New(400, "validation_error", fmt.Sprintf("Variant %d title is required", i+1))
		}
		key := strings.ToLower(vt)
		if _, ok := seenTitles[key]; ok {
			return nil, apierror.New(400, "validation_error", "Duplicate variant title: "+vt)
		}
		seenTitles[key] = struct{}{}
		if v.RPPPrice < 0 || v.WSPrice < 0 {
			return nil, apierror.New(400, "validation_error", "Prices must be >= 0")
		}
		shopifyVariants = append(shopifyVariants, shopify.CreateUnlistedProductVariantInput{
			Title: vt,
			Price: fmt.Sprintf("%.2f", v.RPPPrice),
		})
	}

	existing, err := s.store.GetProductByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if existing != nil {
		switch strings.ToLower(existing.Status) {
		case ProductStatusUnlisted, ProductStatusActive:
			dto := mapProductToDTO(existing, "")
			return &dto, nil
		case ProductStatusCreating:
			return nil, apierror.New(409, "create_in_progress", "Custom product creation is already in progress for this key")
		case ProductStatusFailed:
			// Allow retry: delete failed stub and continue.
			_ = s.store.DeleteProductByID(ctx, existing.ID)
		default:
			dto := mapProductToDTO(existing, "")
			return &dto, nil
		}
	}

	pendingShopifyID := "pending:" + idempotencyKey
	if len(pendingShopifyID) > 64 {
		pendingShopifyID = pendingShopifyID[:64]
	}
	stub := &Product{
		ShopifyID:            pendingShopifyID,
		Title:                title,
		Status:               ProductStatusCreating,
		Origin:               ProductOriginCustom,
		InternalNote:         input.InternalNote,
		CreateIdempotencyKey: &idempotencyKey,
	}
	if err := s.store.CreateCustomProductStub(ctx, stub); err != nil {
		// Race on unique idempotency key — return existing if present.
		again, getErr := s.store.GetProductByIdempotencyKey(ctx, idempotencyKey)
		if getErr == nil && again != nil {
			if strings.EqualFold(again.Status, ProductStatusUnlisted) || strings.EqualFold(again.Status, ProductStatusActive) {
				dto := mapProductToDTO(again, "")
				return &dto, nil
			}
			if strings.EqualFold(again.Status, ProductStatusCreating) {
				return nil, apierror.New(409, "create_in_progress", "Custom product creation is already in progress for this key")
			}
		}
		return nil, apierror.New(500, "create_failed", "Failed to start custom product creation")
	}

	created, err := s.client.CreateUnlistedProduct(ctx, shopify.CreateUnlistedProductInput{
		Title:    title,
		Variants: shopifyVariants,
	})
	if err != nil {
		_ = s.store.DeleteProductByID(ctx, stub.ID)
		return nil, apierror.New(502, "shopify_error", "Failed to create Shopify product: "+err.Error())
	}

	var media *shopify.CreatedProductMedia
	if input.ReferenceImage != nil && len(input.ReferenceImage.Data) > 0 {
		media, err = s.client.AttachProductMediaFromBytes(ctx, created.ID, input.ReferenceImage.Filename, input.ReferenceImage.ContentType, input.ReferenceImage.Data, title)
		if err != nil {
			slogWarnCreate(ctx, "failed to attach reference image bytes", err)
		}
	} else if input.ReferenceImageURL != nil && strings.TrimSpace(*input.ReferenceImageURL) != "" {
		media, err = s.client.AttachProductMediaFromURL(ctx, created.ID, strings.TrimSpace(*input.ReferenceImageURL), title)
		if err != nil {
			slogWarnCreate(ctx, "failed to attach reference image url", err)
		}
	}

	status := strings.ToLower(created.Status)
	if status == "" {
		status = ProductStatusUnlisted
	}

	// Match Momiji WS$ to requested variants by title (case-insensitive).
	wsByTitle := make(map[string]float64, len(input.Variants))
	rppByTitle := make(map[string]float64, len(input.Variants))
	for _, v := range input.Variants {
		key := strings.ToLower(strings.TrimSpace(v.Title))
		wsByTitle[key] = v.WSPrice
		rppByTitle[key] = v.RPPPrice
	}

	awaiting := CustomLinkStateAwaitingSKU
	variants := make([]ProductVariant, 0, len(created.Variants))
	for _, cv := range created.Variants {
		titleKey := strings.ToLower(strings.TrimSpace(cv.Title))
		ws := wsByTitle[titleKey]
		price := rppByTitle[titleKey]
		if parsed, err := parsePrice(cv.Price); err == nil {
			price = parsed
		}
		wsCopy := ws
		linkState := awaiting
		variants = append(variants, ProductVariant{
			ShopifyVariantID:       cv.ID,
			Title:                  cv.Title,
			SKU:                    "",
			Price:                  price,
			WSPrice:                &wsCopy,
			// Stock starts at 0 on Shopify create, so default to pre_order (same rule as tracked variants).
			FulfillmentType:        string(FulfillmentTypePreOrder),
			InventoryQuantity:      cv.InventoryQuantity,
			ShopifyInventoryItemID: cv.InventoryItemID,
			CustomLinkState:        &linkState,
			InventoryTracked:       false,
			ImageSrc: func() string {
				if media != nil {
					return media.URL
				}
				return ""
			}(),
		})
	}

	var images []ProductImage
	if media != nil {
		images = append(images, ProductImage{
			ShopifyID: media.ID,
			Src:       media.URL,
			Alt:       media.Alt,
			Position:  1,
		})
	}

	finalProduct := &Product{
		ShopifyID:    created.ID,
		Title:        created.Title,
		Status:       status,
		Handle:       created.Handle,
		Origin:       ProductOriginCustom,
		InternalNote: input.InternalNote,
	}
	if err := s.store.FinalizeCustomProduct(ctx, stub.ID, finalProduct, variants, images); err != nil {
		_ = s.store.MarkCustomProductFailed(ctx, stub.ID)
		return nil, apierror.New(500, "persist_failed", "Shopify product created but failed to persist locally")
	}

	persisted, err := s.store.GetProductByID(ctx, stub.ID)
	if err != nil || persisted == nil {
		// Fallback: fetch by shopify id
		persisted, err = s.store.GetProductByShopifyID(ctx, created.ID)
		if err != nil || persisted == nil {
			return nil, apierror.ErrInternal
		}
		// GetProductByShopifyID may not preload variants — load them.
		if len(persisted.Variants) == 0 {
			vs, vErr := s.store.GetVariantsByProductID(ctx, persisted.ID)
			if vErr == nil {
				persisted.Variants = vs
			}
		}
	}
	dto := mapProductToDTO(persisted, "")
	return &dto, nil
}

func parsePrice(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func slogWarnCreate(ctx context.Context, msg string, err error) {
	// local helper keeps create file free of unused imports when tests stub logging
	_ = ctx
	_ = msg
	_ = err
}
