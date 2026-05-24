package product

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)


type ProductService interface {
	GetVariantByID(ctx context.Context, variantID string) (*VariantDTO, error)
	GetVariants(ctx context.Context) ([]VariantDTO, error)
	SyncFromShopify(ctx context.Context) error
	GetProductByID(ctx context.Context, id string) (*ProductDetailDTO, error)
	GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error)
	UpdateProductStatus(ctx context.Context, productID string, status string) error
	UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error
	UpdateVariantPrice(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error
}

type service struct {
	store  Store
	client shopify.Client
}

func NewProductService(store Store, client shopify.Client) ProductService {
	return &service{store: store, client: client}
}

func (s *service) GetVariantByID(ctx context.Context, variantID string) (*VariantDTO, error) {
	variant, err := s.store.GetVariantByShopifyID(ctx, variantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if variant == nil {
		return nil, apierror.ErrNotFound
	}
	return mapVariantToDTO(variant), nil
}

func (s *service) GetVariants(ctx context.Context) ([]VariantDTO, error) {
	variants, err := s.store.GetVariants(ctx)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	
	dtos := make([]VariantDTO, len(variants))
	for i, v := range variants {
		dtos[i] = *mapVariantToDTO(&v)
	}
	return dtos, nil
}

func mapVariantToDTO(variant *ProductVariant) *VariantDTO {
	wsPrice := "0.00"
	if variant.WSPrice != nil {
		wsPrice = fmt.Sprintf("%.2f", *variant.WSPrice)
	} else {
		wsPrice = fmt.Sprintf("%.2f", variant.Price)
	}

	retailPrice := "0.00"
	if variant.RetailPrice != nil {
		retailPrice = fmt.Sprintf("%.2f", *variant.RetailPrice)
	} else {
		retailPrice = fmt.Sprintf("%.2f", variant.Price)
	}

	return &VariantDTO{
		ID:              variant.ShopifyVariantID,
		Title:           variant.Title,
		ImageSrc:        variant.ImageSrc,
		RetailPrice:     retailPrice,
		WSPrice:         wsPrice,
		FulfillmentType: FulfillmentType(variant.FulfillmentType),
	}
}

const shopifySyncPageCap = 10

func (s *service) SyncFromShopify(ctx context.Context) error {
	query := `
		query($cursor: String) {
		  products(first: 50, after: $cursor) {
			pageInfo { hasNextPage endCursor }
			edges {
			  node {
				id title descriptionHtml status
				variants(first: 10) {
				  edges {
					node { id title sku price image { url } }
				  }
				}
			  }
			}
		  }
		}
	`

	var cursor *string
	for page := 0; page < shopifySyncPageCap; page++ {
		vars := map[string]interface{}{"cursor": cursor}

		resBytes, err := s.client.QueryAdminGraphQL(ctx, query, vars)
		if err != nil {
			return fmt.Errorf("shopify graphql query failed (page %d): %w", page+1, err)
		}

		var res struct {
			Data struct {
				Products struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Edges []struct {
						Node struct {
							ID              string `json:"id"`
							Title           string `json:"title"`
							DescriptionHtml string `json:"descriptionHtml"`
							Status          string `json:"status"`
							Variants        struct {
								Edges []struct {
									Node struct {
										ID    string `json:"id"`
										Title string `json:"title"`
										Sku   string `json:"sku"`
										Price string `json:"price"`
										Image struct{ Url string `json:"url"` } `json:"image"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"variants"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"products"`
			} `json:"data"`
		}

		if err := json.Unmarshal(resBytes, &res); err != nil {
			return fmt.Errorf("failed to parse shopify response (page %d): %w", page+1, err)
		}

		for _, pEdge := range res.Data.Products.Edges {
			pNode := pEdge.Node
			product := &Product{
				ShopifyID:   pNode.ID,
				Title:       pNode.Title,
				Description: pNode.DescriptionHtml,
				Status:      pNode.Status,
			}
			if err := s.store.UpsertProduct(ctx, product); err != nil {
				return fmt.Errorf("failed to upsert product %s: %w", product.ShopifyID, err)
			}

			p, err := s.store.GetProductByShopifyID(ctx, product.ShopifyID)
			if err != nil {
				return fmt.Errorf("failed to reload product %s: %w", product.ShopifyID, err)
			}
			if p == nil {
				slog.WarnContext(ctx, "product not found after upsert, skipping variants",
					slog.String("shopify_id", product.ShopifyID))
				continue
			}

			for _, vEdge := range pNode.Variants.Edges {
				vNode := vEdge.Node
				price, _ := strconv.ParseFloat(vNode.Price, 64)
				variant := &ProductVariant{
					ProductID:        p.ID,
					ShopifyVariantID: vNode.ID,
					Title:            vNode.Title,
					SKU:              vNode.Sku,
					Price:            price,
					ImageSrc:         vNode.Image.Url,
					FulfillmentType:  string(FulfillmentTypeShipReady),
				}
				if err := s.store.UpsertVariant(ctx, variant); err != nil {
					return fmt.Errorf("failed to upsert variant %s: %w", variant.ShopifyVariantID, err)
				}
			}
		}

		if !res.Data.Products.PageInfo.HasNextPage {
			break
		}
		endCursor := res.Data.Products.PageInfo.EndCursor
		cursor = &endCursor

		if page == shopifySyncPageCap-1 {
			slog.WarnContext(ctx, "shopify sync page cap reached, some products may be missing",
				slog.Int("cap", shopifySyncPageCap))
		}
	}

	return nil
}

var validProductStatuses = map[string]bool{"active": true, "draft": true, "archived": true}

func (s *service) GetProductByID(ctx context.Context, id string) (*ProductDetailDTO, error) {
	p, err := s.store.GetProductByID(ctx, id)
	if err != nil { return nil, apierror.ErrInternal }
	if p == nil { return nil, apierror.ErrNotFound }
	return &ProductDetailDTO{ID: p.ID, ShopifyID: p.ShopifyID, Title: p.Title, Description: p.Description, Status: p.Status}, nil
}

func (s *service) GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error) {
	variants, err := s.store.GetVariantsByProductID(ctx, productID)
	if err != nil { return nil, apierror.ErrInternal }
	dtos := make([]VariantDTO, len(variants))
	for i, v := range variants { dtos[i] = *mapVariantToDTO(&v) }
	return dtos, nil
}

func (s *service) UpdateProductStatus(ctx context.Context, productID string, status string) error {
	if !validProductStatuses[status] {
		return apierror.New(400, "validation_error", "status must be one of: active, draft, archived")
	}
	if err := s.store.UpdateProductStatus(ctx, productID, status); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error {
	if err := s.store.UpdateVariantBatchLabel(ctx, productID, batchLabel); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) UpdateVariantPrice(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error {
	if wsPrice == nil && retailPrice == nil {
		return apierror.New(400, "validation_error", "at least one of ws_price or retail_price must be provided")
	}
	if err := s.store.UpdateVariantPrices(ctx, variantID, wsPrice, retailPrice); err != nil {
		return apierror.ErrInternal
	}
	return nil
}
