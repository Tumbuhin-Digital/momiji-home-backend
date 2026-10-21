package shopify

import (
	"context"
	"strings"
	"testing"
)

func TestGetVariantInventoryPoliciesReadsPolicyAndProduct(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{"VariantInventoryPolicies": `{"data":{"nodes":[
		{"id":"gid://shopify/ProductVariant/501","inventoryPolicy":"DENY","product":{"id":"gid://shopify/Product/77"}},
		{"id":"gid://shopify/ProductVariant/502","inventoryPolicy":"CONTINUE","product":{"id":"gid://shopify/Product/78"}},
		null
	]}}`})

	got, err := client.GetVariantInventoryPolicies(context.Background(), []string{
		"gid://shopify/ProductVariant/501", "gid://shopify/ProductVariant/502", "gid://shopify/ProductVariant/503",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["gid://shopify/ProductVariant/501"] != (VariantInventoryPolicy{ProductID: "gid://shopify/Product/77", Policy: InventoryPolicyDeny}) ||
		got["gid://shopify/ProductVariant/502"].Policy != InventoryPolicyContinue || len(got) != 2 {
		t.Fatalf("unexpected policies: %+v", got)
	}
}

func TestSetVariantsInventoryPolicySendsEachVariant(t *testing.T) {
	client, sent := cannedClient(t, map[string]string{"productVariantsBulkUpdate": `{"data":{"productVariantsBulkUpdate":{"userErrors":[]}}}`})

	err := client.SetVariantsInventoryPolicy(context.Background(), "gid://shopify/Product/77",
		[]string{"gid://shopify/ProductVariant/501", "gid://shopify/ProductVariant/504"}, InventoryPolicyContinue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vars := (*sent)[0]
	variants, _ := vars["variants"].([]interface{})
	if vars["productId"] != "gid://shopify/Product/77" || len(variants) != 2 {
		t.Fatalf("unexpected variables: %+v", vars)
	}
	first, _ := variants[0].(map[string]interface{})
	if first["id"] != "gid://shopify/ProductVariant/501" || first["inventoryPolicy"] != "CONTINUE" {
		t.Fatalf("unexpected variant input: %+v", first)
	}
}

func TestSetVariantsInventoryPolicyReportsUserErrors(t *testing.T) {
	client, _ := cannedClient(t, map[string]string{"productVariantsBulkUpdate": `{"data":{"productVariantsBulkUpdate":{"userErrors":[{"field":["variants"],"message":"Product does not exist"}]}}}`})

	err := client.SetVariantsInventoryPolicy(context.Background(), "gid://shopify/Product/77", []string{"gid://shopify/ProductVariant/501"}, InventoryPolicyDeny)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected the user error, got %v", err)
	}
}
