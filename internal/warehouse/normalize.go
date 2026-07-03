package warehouse

import (
	"context"
	"log/slog"
	"strings"
)

// NormalizeCode maps a warehouse code to east or west without logging.
func NormalizeCode(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case CodeWest:
		return CodeWest
	default:
		return CodeEast
	}
}

// NormalizeOrigin maps warehouse codes to east/west and warns on unrecognized non-empty values.
func NormalizeOrigin(ctx context.Context, code string, attrs ...any) string {
	trimmed := strings.ToLower(strings.TrimSpace(code))
	if trimmed != "" && trimmed != CodeEast && trimmed != CodeWest {
		logAttrs := []any{
			slog.String("warehouse_origin", code),
			slog.String("normalized_to", CodeEast),
		}
		logAttrs = append(logAttrs, attrs...)
		if ctx != nil {
			slog.WarnContext(ctx, "unknown warehouse origin, defaulting to east", logAttrs...)
		} else {
			slog.Warn("unknown warehouse origin, defaulting to east", logAttrs...)
		}
	}
	return NormalizeCode(code)
}
