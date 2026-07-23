package order

import "testing"

func TestEffectiveItemsReceived(t *testing.T) {
	tests := []struct {
		name string
		item OrderItem
		want int
	}{
		{
			name: "uses stored received",
			item: OrderItem{Quantity: 4, ItemsReceived: 2, ItemStatus: "shipped"},
			want: 2,
		},
		{
			name: "delivered status fills quantity when received missing",
			item: OrderItem{Quantity: 4, ItemsReceived: 0, ItemStatus: "delivered", Type: "pre_order", FulfillmentStep: 5},
			want: 4,
		},
		{
			name: "preorder step 5 fills quantity",
			item: OrderItem{Quantity: 4, ItemsReceived: 0, ItemStatus: "shipped", Type: "pre_order", FulfillmentStep: 5},
			want: 4,
		},
		{
			name: "shipped but not delivered stays 0",
			item: OrderItem{Quantity: 4, ItemsReceived: 0, ItemStatus: "shipped", Type: "pre_order", FulfillmentStep: 4},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveItemsReceived(tt.item); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}
