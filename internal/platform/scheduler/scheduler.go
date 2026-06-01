package scheduler

import (
	"context"
	"time"
)

func StartDailyJob(ctx context.Context, fn func(ctx context.Context)) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fn(ctx)
			}
		}
	}()
}
