package shipping

import "math"

// SplitHalf divides a shipping total into an upfront half and a remaining half.
// The remainder is derived by subtraction rather than rounded independently, so
// upfront + remaining always equals the rounded total exactly — no stray cent.
func SplitHalf(total float64) (upfront, remaining float64) {
	total = math.Round(total*100) / 100
	if total <= 0 {
		return 0, 0
	}
	upfront = math.Round(total*50) / 100
	remaining = math.Round((total-upfront)*100) / 100
	return upfront, remaining
}
