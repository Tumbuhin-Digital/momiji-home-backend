package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client interface {
	QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error)
	CreateDraftOrder(ctx context.Context, input DraftOrderInput) (*DraftOrderResponse, error)
	CreateStorefrontCheckout(ctx context.Context, input CheckoutCreateInput) (*CheckoutResponse, error)
}

type clientImpl struct {
	StoreDomain     string
	AdminToken      string
	StorefrontToken string
	HTTPClient      *http.Client
}

func NewClient(storeDomain, adminToken, storefrontToken string) Client {
	return &clientImpl{
		StoreDomain:     storeDomain,
		AdminToken:      adminToken,
		StorefrontToken: storefrontToken,
		HTTPClient:      &http.Client{},
	}
}

func (c *clientImpl) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	url := fmt.Sprintf("https://%s/admin/api/2024-01/graphql.json", c.StoreDomain)

	payload := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.AdminToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shopify admin graphql error: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type DraftOrderInput struct {
	LineItems []DraftOrderLineItem `json:"lineItems"`
	Customer  *DraftOrderCustomer  `json:"customer,omitempty"`
}

type DraftOrderLineItem struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

type DraftOrderCustomer struct {
	Email string `json:"email"`
}

type DraftOrderResponse struct {
	ID        string `json:"id"`
	InvoiceUrl string `json:"invoiceUrl"`
}

func (c *clientImpl) CreateDraftOrder(ctx context.Context, input DraftOrderInput) (*DraftOrderResponse, error) {
	query := `
		mutation draftOrderCreate($input: DraftOrderInput!) {
		  draftOrderCreate(input: $input) {
			draftOrder {
			  id
			  invoiceUrl
			}
			userErrors {
			  field
			  message
			}
		  }
		}
	`
	vars := map[string]interface{}{"input": input}
	
	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			DraftOrderCreate struct {
				DraftOrder *DraftOrderResponse `json:"draftOrder"`
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"draftOrderCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, err
	}
	if len(res.Data.DraftOrderCreate.UserErrors) > 0 {
		return nil, fmt.Errorf("shopify draft order error: %s", res.Data.DraftOrderCreate.UserErrors[0].Message)
	}
	if res.Data.DraftOrderCreate.DraftOrder == nil {
		return nil, fmt.Errorf("failed to create draft order")
	}

	return res.Data.DraftOrderCreate.DraftOrder, nil
}

type CheckoutCreateInput struct {
	Email     string             `json:"email"`
	LineItems []CheckoutLineItem `json:"lineItems"`
}

type CheckoutLineItem struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

type CheckoutResponse struct {
	ID     string `json:"id"`
	WebUrl string `json:"webUrl"`
}

func (c *clientImpl) QueryStorefrontGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	url := fmt.Sprintf("https://%s/api/2024-01/graphql.json", c.StoreDomain)

	payload := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Storefront-Access-Token", c.StorefrontToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("shopify storefront graphql error: status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (c *clientImpl) CreateStorefrontCheckout(ctx context.Context, input CheckoutCreateInput) (*CheckoutResponse, error) {
	query := `
		mutation checkoutCreate($input: CheckoutCreateInput!) {
		  checkoutCreate(input: $input) {
			checkout {
			  id
			  webUrl
			}
			checkoutUserErrors {
			  message
			}
		  }
		}
	`
	vars := map[string]interface{}{"input": input}
	
	resBytes, err := c.QueryStorefrontGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			CheckoutCreate struct {
				Checkout           *CheckoutResponse `json:"checkout"`
				CheckoutUserErrors []struct {
					Message string `json:"message"`
				} `json:"checkoutUserErrors"`
			} `json:"checkoutCreate"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, err
	}
	if len(res.Data.CheckoutCreate.CheckoutUserErrors) > 0 {
		return nil, fmt.Errorf("shopify checkout error: %s", res.Data.CheckoutCreate.CheckoutUserErrors[0].Message)
	}
	if res.Data.CheckoutCreate.Checkout == nil {
		return nil, fmt.Errorf("failed to create checkout")
	}

	return res.Data.CheckoutCreate.Checkout, nil
}
