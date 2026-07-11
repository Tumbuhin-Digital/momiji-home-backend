package shipping

import "math"

// BufferPercent is the markup applied on top of every carrier shipping rate.
const BufferPercent = 10

// ApplyShippingBuffer rounds the raw carrier amount to cents, applies BufferPercent,
// and returns base, buffer residual, and buffered total such that
// base + bufferAmount == buffered at 2-decimal precision.
func ApplyShippingBuffer(raw float64) (base, bufferAmount, buffered float64) {
	base = math.Round(raw*100) / 100
	buffered = math.Round(base*(1+float64(BufferPercent)/100)*100) / 100
	bufferAmount = math.Round((buffered-base)*100) / 100
	return base, bufferAmount, buffered
}
