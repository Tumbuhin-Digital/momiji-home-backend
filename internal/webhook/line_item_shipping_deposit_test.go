package webhook

import "testing"

func TestIsPreorderShippingDepositLineItem(t *testing.T) {
	tests := []struct {
		name string
		item ShopifyOrderLineItem
		want bool
	}{
		{
			name: "shipping deposit line",
			item: ShopifyOrderLineItem{
				Title:      "Shipping (Pre-Order) - 50%",
				Properties: []ShopifyProperty{{Name: "charge_type", Value: "pre_order_shipping_deposit"}},
			},
			want: true,
		},
		{
			name: "preorder product deposit is not a shipping charge",
			item: ShopifyOrderLineItem{
				Title:      "[PREORDER] Chair (Deposit 50%)",
				Properties: []ShopifyProperty{{Name: "type", Value: "preorder_dp"}},
			},
			want: false,
		},
		{
			name: "settlement shipping line is not the checkout deposit",
			item: ShopifyOrderLineItem{
				Properties: []ShopifyProperty{{Name: "charge_type", Value: "pre_order_shipping"}},
			},
			want: false,
		},
		{
			name: "plain product line",
			item: ShopifyOrderLineItem{VariantID: 123, Title: "Chair"},
			want: false,
		},
		{
			name: "non-string property value",
			item: ShopifyOrderLineItem{
				Properties: []ShopifyProperty{{Name: "charge_type", Value: 42}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPreorderShippingDepositLineItem(tt.item); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
