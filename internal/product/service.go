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
	GetProducts(ctx context.Context, query ProductQuery) ([]ProductDTO, int64, error)
	SyncFromShopify(ctx context.Context) error
	GetProductByID(ctx context.Context, id string) (*ProductDTO, error)
	GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error)
	UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) (*ProductDTO, error)
	UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) (*ProductDTO, error)
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

func (s *service) GetProducts(ctx context.Context, query ProductQuery) ([]ProductDTO, int64, error) {
	products, total, err := s.store.GetProducts(ctx, query)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}
	
	dtos := make([]ProductDTO, len(products))
	for i, p := range products {
		dtos[i] = mapProductToDTO(&p)
	}
	return dtos, total, nil
}

func mapProductToDTO(p *Product) ProductDTO {
	variants := make([]VariantDTO, len(p.Variants))
	for i, v := range p.Variants {
		variants[i] = *mapVariantToDTO(&v)
	}
	
	images := make([]ProductImageDTO, len(p.Images))
	for i, img := range p.Images {
		images[i] = ProductImageDTO{
			ID:       img.ShopifyID,
			Src:      img.Src,
			Alt:      img.Alt,
			Position: img.Position,
		}
	}
	if images == nil { images = []ProductImageDTO{} }

	// Determine product-level preorder_batch_label and expected_ship_date from first variant
	var batchLabel, shipDate *string
	if len(p.Variants) > 0 {
		if p.Variants[0].PreorderBatchLabel != nil {
			batchLabel = p.Variants[0].PreorderBatchLabel
		}
		if p.Variants[0].ExpectedShipDate != nil {
			dt := p.Variants[0].ExpectedShipDate.Format("2006-01-02T15:04:05Z07:00")
			shipDate = &dt
		}
	}
	
	return ProductDTO{
		ID:                 p.ID,
		ShopifyID:          p.ShopifyID,
		Handle:             p.Handle,
		Title:              p.Title,
		Description:        p.Description,
		Vendor:             p.Vendor,
		ProductType:        p.ProductType,
		Tags:               p.Tags,
		Status:             p.Status,
		PreorderBatchLabel: batchLabel,
		ExpectedShipDate:   shipDate,
		BodyHTML:           p.BodyHTML,
		Variants:           variants,
		Images:             images,
	}
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

	var sku *string
	if variant.SKU != "" {
		skuStr := variant.SKU
		sku = &skuStr
	}

	return &VariantDTO{
		ID:                variant.ShopifyVariantID,
		Title:             variant.Title,
		SKU:               sku,
		ImageSrc:          variant.ImageSrc,
		RetailPrice:       retailPrice,
		WSPrice:           wsPrice,
		FulfillmentType:   FulfillmentType(variant.FulfillmentType),
		InventoryQuantity: variant.InventoryQuantity,
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
					node { id title sku price inventoryQuantity image { url } }
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
										ID                string `json:"id"`
										Title             string `json:"title"`
										Sku               string `json:"sku"`
										Price             string `json:"price"`
										InventoryQuantity int    `json:"inventoryQuantity"`
										Image             struct{ Url string `json:"url"` } `json:"image"`
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
					SKU:               vNode.Sku,
					Price:             price,
					ImageSrc:          vNode.Image.Url,
					FulfillmentType:   string(FulfillmentTypeShipReady),
					InventoryQuantity: vNode.InventoryQuantity,
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

func (s *service) GetProductByID(ctx context.Context, id string) (*ProductDTO, error) {
	p, err := s.store.GetProductByID(ctx, id)
	if err != nil { return nil, apierror.ErrInternal }
	if p == nil { return nil, apierror.ErrNotFound }
	dto := mapProductToDTO(p)
	return &dto, nil
}

func (s *service) GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error) {
	variants, err := s.store.GetVariantsByProductID(ctx, productID)
	if err != nil { return nil, apierror.ErrInternal }
	dtos := make([]VariantDTO, len(variants))
	for i, v := range variants { dtos[i] = *mapVariantToDTO(&v) }
	return dtos, nil
}

func (s *service) UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) (*ProductDTO, error) {
	if fulfillmentType != "ship_ready" && fulfillmentType != "pre_order" {
		return nil, apierror.New(400, "validation_error", "fulfillment_type must be ship_ready or pre_order")
	}
	if err := s.store.UpdateProductStatus(ctx, productID, fulfillmentType); err != nil {
		return nil, apierror.ErrInternal
	}
	return s.GetProductByID(ctx, productID)
}

func (s *service) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) (*ProductDTO, error) {
	if err := s.store.UpdateVariantBatchLabel(ctx, productID, batchLabel, expectedShipDate); err != nil {
		return nil, apierror.ErrInternal
	}
	return s.GetProductByID(ctx, productID)
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
