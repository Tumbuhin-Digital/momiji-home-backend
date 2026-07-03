package warehouse

import (
	"context"
	"testing"
)

func TestNormalizeOrigin_UnknownValueFallsBackEast(t *testing.T) {
	got := NormalizeOrigin(context.Background(), "north", "order_id", "ord-1")
	if got != CodeEast {
		t.Fatalf("expected east, got %s", got)
	}
}

func TestNormalizeOrigin_KnownValues(t *testing.T) {
	if got := NormalizeOrigin(context.Background(), "west", "order_id", "ord-1"); got != CodeWest {
		t.Fatalf("expected west, got %s", got)
	}
	if got := NormalizeOrigin(context.Background(), "east", "order_id", "ord-1"); got != CodeEast {
		t.Fatalf("expected east, got %s", got)
	}
	if got := NormalizeOrigin(context.Background(), "", "order_id", "ord-1"); got != CodeEast {
		t.Fatalf("expected default east, got %s", got)
	}
}
