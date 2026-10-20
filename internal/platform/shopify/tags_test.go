package shopify

import (
	"reflect"
	"testing"
)

func TestReplaceManagedTags_KeepsAdminTagsAndSwapsManagedOnes(t *testing.T) {
	got := ReplaceManagedTags(
		[]string{"vip", TagPreorderBalance, "deposit-order-999", TagPreorderDeposit},
		TagPreorderBalance, DepositOrderTag("1323"),
	)
	want := []string{"vip", TagPreorderBalance, "deposit-order-1323"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReplaceManagedTags = %v, want %v", got, want)
	}
}

func TestReplaceManagedTags_NeverNilSoUpdatesClearStaleTags(t *testing.T) {
	got := ReplaceManagedTags([]string{TagPreorderDeposit})
	if got == nil || len(got) != 0 {
		t.Fatalf("expected an empty non-nil list, got %#v", got)
	}
}
