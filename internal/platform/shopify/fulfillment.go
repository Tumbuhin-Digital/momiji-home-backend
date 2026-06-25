package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FulfillmentOrderData represents a Shopify FulfillmentOrder with line items.
type FulfillmentOrderData struct {
	ID                   string
	Status               string
	AssignedLocationName string
	LineItems            []FulfillmentOrderLineItemData
}

type FulfillmentOrderLineItemData struct {
	ID                string
	VariantGID        string
	LineItemTitle     string
	TotalQuantity     int
	RemainingQuantity int
}

// FulfillmentV2LineItemInput maps a FOLI to a quantity for fulfillmentCreateV2.
type FulfillmentV2LineItemInput struct {
	FulfillmentOrderID     string
	FulfillmentOrderLineItems []struct {
		ID       string
		Quantity int
	}
}

// CreateFulfillmentV2Input is the input for creating a partial fulfillment.
type CreateFulfillmentV2Input struct {
	LineItemsByFulfillmentOrder []FulfillmentV2LineItemInput
	TrackingCompany             string
	TrackingNumber              string
	TrackingURL                 string
	NotifyCustomer              bool
}

type CreateFulfillmentV2Result struct {
	FulfillmentID string
}

func orderGID(shopifyOrderID string) string {
	if strings.HasPrefix(shopifyOrderID, "gid://") {
		return shopifyOrderID
	}
	return "gid://shopify/Order/" + shopifyOrderID
}

func (c *clientImpl) FetchFulfillmentOrders(ctx context.Context, shopifyOrderID string) ([]FulfillmentOrderData, error) {
	query := `
		query($orderId: ID!) {
		  order(id: $orderId) {
			fulfillmentOrders(first: 20) {
			  edges {
				node {
				  id
				  status
				  assignedLocation {
					name
				  }
				  lineItems(first: 50) {
					edges {
					  node {
						id
						totalQuantity
						remainingQuantity
						lineItem {
						  title
						  variant {
							id
						  }
						}
					  }
					}
				  }
				}
			  }
			}
		  }
		}
	`
	vars := map[string]interface{}{"orderId": orderGID(shopifyOrderID)}

	resBytes, err := c.QueryAdminGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data struct {
			Order struct {
				FulfillmentOrders struct {
					Edges []struct {
						Node struct {
							ID               string `json:"id"`
							Status           string `json:"status"`
							AssignedLocation *struct {
								Name string `json:"name"`
							} `json:"assignedLocation"`
							LineItems struct {
								Edges []struct {
									Node struct {
										ID                string `json:"id"`
										TotalQuantity     int    `json:"totalQuantity"`
										RemainingQuantity int    `json:"remainingQuantity"`
										LineItem          struct {
											Title   string `json:"title"`
											Variant struct {
												ID string `json:"id"`
											} `json:"variant"`
										} `json:"lineItem"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"lineItems"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"fulfillmentOrders"`
			} `json:"order"`
		} `json:"data"`
	}

	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to parse fulfillment orders: %w", err)
	}

	var result []FulfillmentOrderData
	for _, edge := range res.Data.Order.FulfillmentOrders.Edges {
		fo := FulfillmentOrderData{
			ID:     edge.Node.ID,
			Status: edge.Node.Status,
		}
		if edge.Node.AssignedLocation != nil {
			fo.AssignedLocationName = edge.Node.AssignedLocation.Name
		}
		for _, liEdge := range edge.Node.LineItems.Edges {
			fo.LineItems = append(fo.LineItems, FulfillmentOrderLineItemData{
				ID:                liEdge.Node.ID,
				VariantGID:        liEdge.Node.LineItem.Variant.ID,
				LineItemTitle:     liEdge.Node.LineItem.Title,
				TotalQuantity:     liEdge.Node.TotalQuantity,
				RemainingQuantity: liEdge.Node.RemainingQuantity,
			})
		}
		result = append(result, fo)
	}
	return result, nil
}

func (c *clientImpl) CreateFulfillmentV2(ctx context.Context, input CreateFulfillmentV2Input) (*CreateFulfillmentV2Result, error) {
	var lineItemsByFO []map[string]interface{}
	for _, fo := range input.LineItemsByFulfillmentOrder {
		var folis []map[string]interface{}
		for _, li := range fo.FulfillmentOrderLineItems {
			folis = append(folis, map[string]interface{}{
				"id":       li.ID,
				"quantity": li.Quantity,
			})
		}
		lineItemsByFO = append(lineItemsByFO, map[string]interface{}{
			"fulfillmentOrderId":        fo.FulfillmentOrderID,
			"fulfillmentOrderLineItems": folis,
		})
	}

	fulfillmentInput := map[string]interface{}{
		"notifyCustomer":              input.NotifyCustomer,
		"lineItemsByFulfillmentOrder": lineItemsByFO,
	}
	if input.TrackingNumber != "" {
		trackingInfo := map[string]interface{}{
			"number": input.TrackingNumber,
		}
		if input.TrackingCompany != "" {
			trackingInfo["company"] = input.TrackingCompany
		}
		if input.TrackingURL != "" {
			trackingInfo["url"] = input.TrackingURL
		}
		fulfillmentInput["trackingInfo"] = trackingInfo
	}

	mut := `
		mutation fulfillmentCreateV2($fulfillment: FulfillmentV2Input!) {
		  fulfillmentCreateV2(fulfillment: $fulfillment) {
			fulfillment {
			  id
			  status
			}
			userErrors {
			  field
			  message
			}
		  }
		}
	`
	mutVars := map[string]interface{}{
		"fulfillment": fulfillmentInput,
	}

	mutRes, err := c.QueryAdminGraphQL(ctx, mut, mutVars)
	if err != nil {
		return nil, err
	}

	var mRes struct {
		Data struct {
			FulfillmentCreateV2 struct {
				Fulfillment *struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"fulfillment"`
				UserErrors []struct {
					Field   []string `json:"field"`
					Message string   `json:"message"`
				} `json:"userErrors"`
			} `json:"fulfillmentCreateV2"`
		} `json:"data"`
	}
	if err := json.Unmarshal(mutRes, &mRes); err != nil {
		return nil, fmt.Errorf("failed to parse fulfillmentCreateV2 response: %w", err)
	}
	if len(mRes.Data.FulfillmentCreateV2.UserErrors) > 0 {
		return nil, fmt.Errorf("shopify fulfillment error: %s", mRes.Data.FulfillmentCreateV2.UserErrors[0].Message)
	}
	if mRes.Data.FulfillmentCreateV2.Fulfillment == nil {
		return nil, fmt.Errorf("shopify fulfillment error: no fulfillment returned. raw: %s", string(mutRes))
	}
	return &CreateFulfillmentV2Result{
		FulfillmentID: mRes.Data.FulfillmentCreateV2.Fulfillment.ID,
	}, nil
}

func (c *clientImpl) CreateFulfillmentEvent(ctx context.Context, shopifyFulfillmentID, status string) error {
	if !strings.HasPrefix(shopifyFulfillmentID, "gid://") {
		shopifyFulfillmentID = "gid://shopify/Fulfillment/" + shopifyFulfillmentID
	}

	mut := `
		mutation fulfillmentEventCreate($fulfillmentEvent: FulfillmentEventInput!) {
		  fulfillmentEventCreate(fulfillmentEvent: $fulfillmentEvent) {
			fulfillmentEvent {
			  id
			  status
			}
			userErrors {
			  field
			  message
			}
		  }
		}
	`
	mutVars := map[string]interface{}{
		"fulfillmentEvent": map[string]interface{}{
			"fulfillmentId": shopifyFulfillmentID,
			"status":        status,
		},
	}

	mutRes, err := c.QueryAdminGraphQL(ctx, mut, mutVars)
	if err != nil {
		return err
	}

	var mRes struct {
		Data struct {
			FulfillmentEventCreate struct {
				UserErrors []struct {
					Message string `json:"message"`
				} `json:"userErrors"`
			} `json:"fulfillmentEventCreate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(mutRes, &mRes); err != nil {
		return fmt.Errorf("failed to parse fulfillmentEventCreate response: %w", err)
	}
	if len(mRes.Data.FulfillmentEventCreate.UserErrors) > 0 {
		return fmt.Errorf("shopify fulfillment event error: %s", mRes.Data.FulfillmentEventCreate.UserErrors[0].Message)
	}
	return nil
}
