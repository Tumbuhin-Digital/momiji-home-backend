package uszip

import (
	"strings"
	"unicode"
)

// NormalizeUSZip trims whitespace and returns the 5-digit base ZIP for US addresses.
// Accepts formats like "12345" and "12345-6789".
func NormalizeUSZip(zip string) (string, bool) {
	clean := strings.TrimSpace(zip)
	if clean == "" {
		return "", false
	}

	digits := make([]byte, 0, 5)
	for _, r := range clean {
		if unicode.IsDigit(r) {
			digits = append(digits, byte(r))
			if len(digits) == 5 {
				break
			}
			continue
		}
		if len(digits) > 0 {
			return "", false
		}
		if r == '-' || r == ' ' {
			continue
		}
		return "", false
	}

	if len(digits) != 5 {
		return "", false
	}

	return string(digits), true
}
