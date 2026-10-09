package market

import (
	"context"
	"time"
)

// Reserve time for the remaining providers, including when the caller has a
// shorter deadline than a provider's own retry loop.
func fallbackContext(ctx context.Context, remaining int, maxWait time.Duration) (context.Context, context.CancelFunc) {
	if remaining > 1 {
		if deadline, ok := ctx.Deadline(); ok {
			share := time.Until(deadline) / time.Duration(remaining)
			if share < maxWait {
				maxWait = share
			}
		}
		return context.WithTimeout(ctx, maxWait)
	}
	return context.WithCancel(ctx)
}
