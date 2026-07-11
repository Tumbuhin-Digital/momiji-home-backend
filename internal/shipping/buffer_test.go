package shipping_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
)

func TestApplyShippingBuffer(t *testing.T) {
	tests := []struct {
		name           string
		raw            float64
		wantBase       float64
		wantBuffer     float64
		wantBuffered   float64
	}{
		{
			name:         "thirty dollars",
			raw:          30,
			wantBase:     30.00,
			wantBuffer:   3.00,
			wantBuffered: 33.00,
		},
		{
			name:         "zero",
			raw:          0,
			wantBase:     0,
			wantBuffer:   0,
			wantBuffered: 0,
		},
		{
			name:         "rounds base then buffers",
			raw:          22.345,
			wantBase:     22.35,
			wantBuffer:   2.24,
			wantBuffered: 24.59, // Round(22.35 * 1.10, 2) = Round(24.585, 2) = 24.59
		},
		{
			name:         "half cent residual identity",
			raw:          10.05,
			wantBase:     10.05,
			wantBuffer:   1.01,
			wantBuffered: 11.06, // Round(10.05 * 1.10, 2) = Round(11.055, 2) = 11.06
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, buffer, buffered := shipping.ApplyShippingBuffer(tt.raw)
			if math.Abs(base-tt.wantBase) > 1e-9 {
				t.Errorf("base = %v, want %v", base, tt.wantBase)
			}
			if math.Abs(buffer-tt.wantBuffer) > 1e-9 {
				t.Errorf("buffer = %v, want %v", buffer, tt.wantBuffer)
			}
			if math.Abs(buffered-tt.wantBuffered) > 1e-9 {
				t.Errorf("buffered = %v, want %v", buffered, tt.wantBuffered)
			}
			if math.Abs(base+buffer-buffered) > 1e-9 {
				t.Errorf("base+buffer (%v) != buffered (%v)", base+buffer, buffered)
			}
			// Display-precision identity after %.2f formatting
			baseS := fmt.Sprintf("%.2f", base)
			bufferS := fmt.Sprintf("%.2f", buffer)
			costS := fmt.Sprintf("%.2f", buffered)
			var b, buf, c float64
			_, _ = fmt.Sscanf(baseS, "%f", &b)
			_, _ = fmt.Sscanf(bufferS, "%f", &buf)
			_, _ = fmt.Sscanf(costS, "%f", &c)
			if math.Abs(b+buf-c) > 1e-9 {
				t.Errorf("formatted base+buffer (%s+%s) != cost (%s)", baseS, bufferS, costS)
			}
		})
	}
}

func TestBufferPercent(t *testing.T) {
	if shipping.BufferPercent != 10 {
		t.Fatalf("BufferPercent = %d, want 10", shipping.BufferPercent)
	}
}
