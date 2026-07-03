package shipping_test

import (
	"context"
	"math"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
)

func TestBuildPackages_ConvertsKgToLbOnce(t *testing.T) {
	ctx := context.Background()
	pkgs := shipping.BuildPackages(ctx, []shipping.PackableUnit{
		{WeightKg: 1.0, BoxCount: 1},
	})
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	want := units.KgToLb(1.0)
	if math.Abs(pkgs[0].Weight.Value-want) > 1e-9 {
		t.Fatalf("expected %f lb, got %f", want, pkgs[0].Weight.Value)
	}
	if pkgs[0].Weight.Unit != "pound" {
		t.Fatalf("expected pound unit, got %q", pkgs[0].Weight.Unit)
	}
}

func TestBuildPackages_ConvertsDimensionsToInches(t *testing.T) {
	ctx := context.Background()
	pkgs := shipping.BuildPackages(ctx, []shipping.PackableUnit{
		{
			WeightKg: 2,
			DepthCm:  100,
			WidthCm:  50,
			HeightCm: 30,
			BoxCount: 1,
		},
	})
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Dimensions == nil {
		t.Fatal("expected dimensions")
	}
	d := pkgs[0].Dimensions
	if d.Unit != "inch" {
		t.Fatalf("expected inch unit, got %q", d.Unit)
	}
	if d.Length != units.CmToIn(100) {
		t.Fatalf("length: got %f want %f", d.Length, units.CmToIn(100))
	}
	if d.Width != units.CmToIn(50) {
		t.Fatalf("width: got %f want %f", d.Width, units.CmToIn(50))
	}
	if d.Height != units.CmToIn(30) {
		t.Fatalf("height: got %f want %f", d.Height, units.CmToIn(30))
	}
}

func TestBuildPackages_MultiBox(t *testing.T) {
	ctx := context.Background()
	pkgs := shipping.BuildPackages(ctx, []shipping.PackableUnit{
		{WeightKg: 5, BoxCount: 3},
	})
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	for i, pkg := range pkgs {
		if pkg.Weight.Unit != "pound" {
			t.Fatalf("package %d: expected pound", i)
		}
		if math.Abs(pkg.Weight.Value-units.KgToLb(5)) > 1e-9 {
			t.Fatalf("package %d: wrong weight", i)
		}
	}
}

func TestPackableUnitFromCartItem_KilogramsPassthrough(t *testing.T) {
	unit := shipping.PackableUnitFromCartItem(3.5, "KILOGRAMS", 10, 20, 30, 2)
	if unit.WeightKg != 3.5 {
		t.Fatalf("expected 3.5 kg, got %f", unit.WeightKg)
	}
	if unit.BoxCount != 2 {
		t.Fatalf("expected box count 2, got %d", unit.BoxCount)
	}
}
