package product

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

// FulfillmentType represents how a product is fulfilled.
type FulfillmentType string

const (
	FulfillmentTypeShipReady FulfillmentType = "ship_ready"
	FulfillmentTypePreOrder  FulfillmentType = "pre_order"
)

type VariantDTO struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	ImageSrc        string          `json:"image_src"`
	RetailPrice     string          `json:"retail_price"`
	WSPrice         string          `json:"ws_price"`
	FulfillmentType FulfillmentType `json:"fulfillment_type"`
}

type ProductService interface {
	GetVariantByID(ctx context.Context, variantID string) (*VariantDTO, error)
	GetVariants(ctx context.Context) ([]VariantDTO, error)
	SyncFromShopify(ctx context.Context) error
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

func (s *service) SyncFromShopify(ctx context.Context) error {
	query := `
		{
		  products(first: 50) {
			edges {
			  node {
				id
				title
				descriptionHtml
				status
				variants(first: 10) {
				  edges {
					node {
					  id
					  title
					  sku
					  price
					  image {
						url
					  }
					}
				  }
				}
			  }
			}
		  }
		}
	`

	resBytes, err := s.client.QueryAdminGraphQL(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("shopify graphql query failed: %w", err)
	}

	var res struct {
		Data struct {
			Products struct {
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
									Image struct {
										Url string `json:"url"`
									} `json:"image"`
								} `json:"node"`
							} `json:"edges"`
						} `json:"variants"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"products"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return fmt.Errorf("failed to parse shopify response: %w", err)
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
		
		// The product might not have its UUID populated if Upsert didn't return it depending on driver, 
		// so let's query it.
		var p Product
		if dbErr := s.store.(*PostgresStore).db.Where("shopify_id = ?", product.ShopifyID).First(&p).Error; dbErr == nil {
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
	}
	
	return nil
}
