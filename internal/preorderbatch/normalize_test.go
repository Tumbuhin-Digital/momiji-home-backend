package preorderbatch

import "testing"

func TestNormalizeShopifyVariantID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"gid://shopify/ProductVariant/48801866416383", "gid://shopify/ProductVariant/48801866416383"},
		{"48801866416383", "gid://shopify/ProductVariant/48801866416383"},
		{" 48801866416383 ", "gid://shopify/ProductVariant/48801866416383"},
		{"not-a-number", "not-a-number"},
	}

	for _, tc := range cases {
		got := normalizeShopifyVariantID(tc.in)
		if got != tc.want {
			t.Fatalf("normalizeShopifyVariantID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
