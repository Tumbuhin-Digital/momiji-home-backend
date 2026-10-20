package shopify

import "strings"

// Tags the app manages on pre-order draft orders. Draft tags carry over to the order
// Shopify creates on payment, so admins can tell a deposit order from the separate
// order that collects its remaining balance.
const (
	TagPreorderDeposit    = "preorder-deposit"
	TagPreorderBalance    = "preorder-balance"
	tagDepositOrderPrefix = "deposit-order-"
)

// DepositOrderTag links a balance order to its deposit order, e.g. "deposit-order-1323".
func DepositOrderTag(depositOrderNumber string) string {
	return tagDepositOrderPrefix + depositOrderNumber
}

// ReplaceManagedTags drops the app-managed tags from existing and appends managed, keeping
// any tag an admin added by hand. It never returns nil, so a draft update always rewrites
// the tag list (clearing a stale managed tag) instead of leaving it untouched.
func ReplaceManagedTags(existing []string, managed ...string) []string {
	out := make([]string, 0, len(existing)+len(managed))
	for _, tag := range existing {
		if isManagedTag(tag) {
			continue
		}
		out = append(out, tag)
	}
	return append(out, managed...)
}

func isManagedTag(tag string) bool {
	return tag == TagPreorderDeposit || tag == TagPreorderBalance || strings.HasPrefix(tag, tagDepositOrderPrefix)
}
