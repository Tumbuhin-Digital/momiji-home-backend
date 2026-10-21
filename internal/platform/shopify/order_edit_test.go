package shopify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// cannedClient answers each GraphQL operation with a fixed body, keyed by a substring of
// the query, and records the variables it was sent.
func cannedClient(t *testing.T, replies map[string]string) (*clientImpl, *[]map[string]interface{}) {
	t.Helper()
	var sent []map[string]interface{}
	client := &clientImpl{
		StoreDomain: "shop.example",
		AdminToken:  "token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			var payload struct {
				Query     string                 `json:"query"`
				Variables map[string]interface{} `json:"variables"`
			}
			_ = json.Unmarshal(body, &payload)
			sent = append(sent, payload.Variables)
			for key, reply := range replies {
				if strings.Contains(payload.Query, key) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(reply)), Header: http.Header{}}, nil
				}
			}
			t.Errorf("unexpected query: %s", payload.Query)
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
		})},
	}
	return client, &sent
}

func TestBeginOrderEditParsesDepositLinesAndTotals(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{"orderEditBegin": `{"data":{"orderEditBegin":{
		"calculatedOrder":{
			"id":"gid://shopify/CalculatedOrder/1",
			"totalOutstandingSet":{"shopMoney":{"amount":"0.0"}},
			"lineItems":{"nodes":[{
				"id":"gid://shopify/CalculatedLineItem/11","title":"Shelf (Deposit 50%)","sku":"MU-1",
				"quantity":3,"editableQuantity":3,"variant":null,
				"customAttributes":[{"key":"type","value":"preorder_dp"},{"key":"variant_ref","value":"gid://shopify/ProductVariant/501"}],
				"originalUnitPriceSet":{"shopMoney":{"amount":"175.0"}},
				"discountedUnitPriceSet":{"shopMoney":{"amount":"175.0"}}
			}]},
			"shippingLines":[{"id":"gid://shopify/CalculatedShippingLine/31","title":"UPS Ground","stagedStatus":"NONE","price":{"shopMoney":{"amount":"86.56"}}}]
		},
		"userErrors":[]}}}`})

	calc, err := client.BeginOrderEdit(context.Background(), "1325")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calc.ID != "gid://shopify/CalculatedOrder/1" || len(calc.LineItems) != 1 || len(calc.ShippingLines) != 1 {
		t.Fatalf("unexpected calculated order: %+v", calc)
	}
	line := calc.LineItems[0]
	if line.VariantID != "" || line.Quantity != 3 || line.DiscountedUnitPrice != 175 || len(line.CustomAttributes) != 2 {
		t.Fatalf("unexpected deposit line: %+v", line)
	}
	if calc.ShippingLines[0].Price != 86.56 {
		t.Fatalf("unexpected shipping line: %+v", calc.ShippingLines[0])
	}
}

func TestOrderEditTreatsGraphQLAndUserErrorsAsFailures(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{
		"orderEditAddVariant": `{"errors":[{"message":"Field 'orderEditAddVariant' doesn't accept argument 'foo'"}]}`,
		"orderEditCommit":     `{"data":{"orderEditCommit":{"order":null,"userErrors":[{"field":["id"],"message":"Order is archived"}]}}}`,
	})

	if _, _, err := client.AddOrderEditVariant(context.Background(), "calc", "variant", "loc", 1); err == nil || !strings.Contains(err.Error(), "doesn't accept") {
		t.Fatalf("expected the GraphQL error, got %v", err)
	}
	if err := client.CommitOrderEdit(context.Background(), "calc", false, "note"); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("expected the user error, got %v", err)
	}
}

func TestAddOrderEditLineDiscountSendsAFixedAmount(t *testing.T) {
	client, sent := cannedClient(t, map[string]string{"orderEditAddLineItemDiscount": `{"data":{"orderEditAddLineItemDiscount":{
		"calculatedLineItem":{"id":"gid://shopify/CalculatedLineItem/new","quantity":3,"variant":{"id":"gid://shopify/ProductVariant/501"},
			"customAttributes":[],"originalUnitPriceSet":{"shopMoney":{"amount":"389.0"}},"discountedUnitPriceSet":{"shopMoney":{"amount":"350.0"}}},
		"calculatedOrder":{"totalOutstandingSet":{"shopMoney":{"amount":"525.0"}}},
		"userErrors":[]}}}`})

	line, totals, err := client.AddOrderEditLineDiscount(context.Background(), "calc", "gid://shopify/CalculatedLineItem/new", "Wholesale price", 39)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if line.DiscountedUnitPrice != 350 || line.OriginalUnitPrice != 389 || totals.Outstanding != 525 {
		t.Fatalf("unexpected result: %+v %+v", line, totals)
	}
	discount := (*sent)[0]["discount"].(map[string]interface{})
	fixed := discount["fixedValue"].(map[string]interface{})
	if fixed["amount"] != "39.00" || fixed["currencyCode"] != "USD" || discount["description"] != "Wholesale price" {
		t.Fatalf("unexpected discount input: %+v", discount)
	}
}

func TestGetOrderBalanceStateParsesLinkLinesAndShipping(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{"OrderBalanceState": `{"data":{"order":{
		"id":"gid://shopify/Order/1325","displayFinancialStatus":"PARTIALLY_PAID",
		"totalOutstandingSet":{"shopMoney":{"amount":"611.56"}},
		"paymentCollectionDetails":{"additionalPaymentCollectionUrl":"https://pay.example/1325"},
		"lineItems":{"nodes":[
			{"id":"gid://shopify/LineItem/11","quantity":3,"currentQuantity":0,"variant":null},
			{"id":"gid://shopify/LineItem/21","quantity":3,"currentQuantity":3,"variant":{"id":"gid://shopify/ProductVariant/501"}}
		]},
		"shippingLines":{"nodes":[{"id":"gid://shopify/ShippingLine/31","title":"UPS Ground - Remaining 50%","originalPriceSet":{"shopMoney":{"amount":"86.56"}}}]}
	}}}`})

	state, err := client.GetOrderBalanceState(context.Background(), "1325")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Outstanding != 611.56 || state.PaymentCollectionURL != "https://pay.example/1325" || state.FinancialStatus != "PARTIALLY_PAID" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if len(state.LineItems) != 2 || state.LineItems[1].VariantID != "gid://shopify/ProductVariant/501" || state.LineItems[0].CurrentQuantity != 0 {
		t.Fatalf("unexpected lines: %+v", state.LineItems)
	}
	if len(state.ShippingLines) != 1 || state.ShippingLines[0].Price != 86.56 {
		t.Fatalf("unexpected shipping: %+v", state.ShippingLines)
	}
}

func TestGetInventoryAtLocationReadsAvailable(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{"InventoryAtLocation": `{"data":{"nodes":[
		{"id":"gid://shopify/InventoryItem/901","tracked":true,"inventoryLevel":{"quantities":[{"name":"available","quantity":3}]}},
		{"id":"gid://shopify/InventoryItem/902","tracked":true,"inventoryLevel":null},
		null
	]}}`})

	got, err := client.GetInventoryAtLocation(context.Background(), []string{"gid://shopify/InventoryItem/901", "gid://shopify/InventoryItem/902", "gid://shopify/InventoryItem/903"}, "gid://shopify/Location/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["gid://shopify/InventoryItem/901"].Available != 3 || got["gid://shopify/InventoryItem/902"].Available != 0 {
		t.Fatalf("unexpected levels: %+v", got)
	}
}

func TestGetPrimaryLocation(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{"PrimaryLocation": `{"data":{"location":{"id":"gid://shopify/Location/91696955647","name":"Manymoons"}}}`})

	got, err := client.GetPrimaryLocation(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "gid://shopify/Location/91696955647" || got.Name != "Manymoons" {
		t.Fatalf("unexpected location: %+v", got)
	}

	empty, _ := cannedClient(t, map[string]string{"PrimaryLocation": `{"data":{"location":null}}`})
	if _, err := empty.GetPrimaryLocation(context.Background()); err == nil {
		t.Fatal("expected an error when Shopify returns no location")
	}
}

func TestGIDNumericID(t *testing.T) {
	cases := map[string]string{
		"gid://shopify/CalculatedLineItem/42":  "42",
		"gid://shopify/LineItem/42":            "42",
		"42":                                   "42",
		"gid://shopify/ShippingLine/7?foo=bar": "7",
		"":                                     "",
	}
	for in, want := range cases {
		if got := GIDNumericID(in); got != want {
			t.Errorf("GIDNumericID(%q) = %q, want %q", in, got, want)
		}
	}
	if LineItemGID(0) != "" || LineItemGID(21) != "gid://shopify/LineItem/21" {
		t.Fatal("unexpected LineItemGID output")
	}
}
