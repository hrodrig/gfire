package engine

import (
	"math/rand"
	"time"
)

const (
	defaultRetryMax = 10
	maxRetryDelay   = time.Hour
	retryBaseDelay  = time.Minute
)

// RetryDelay returns exponential backoff with jitter for attempt (0-based).
func RetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	shift := attempt
	if shift > 30 {
		shift = 30
	}
	delay := retryBaseDelay * time.Duration(1<<shift)
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	jitter := time.Duration(rand.Int63n(int64(time.Second)))
	return delay + jitter
}

func effectiveRetryMax(retryMax int) int {
	if retryMax == 0 {
		return defaultRetryMax
	}
	return retryMax
}
