package device

import "context"

// checkContextNotDone returns ctx.Err() if the provided context is already
// cancelled or expired. A nil context is treated as active.
func checkContextNotDone(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
