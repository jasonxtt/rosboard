package auth

import (
	"testing"
	"time"
)

func TestLoginLimiterCoolsDownWithoutPermanentLock(t *testing.T) {
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(func() time.Time { return now })
	for attempt := 1; attempt <= 4; attempt++ {
		if retry := limiter.failed("key"); retry != 0 {
			t.Fatalf("attempt %d was limited for %s", attempt, retry)
		}
	}
	if retry := limiter.failed("key"); retry != 30*time.Second {
		t.Fatalf("fifth failure retry=%s", retry)
	}
	now = now.Add(31 * time.Second)
	if retry := limiter.retryAfter("key"); retry != 0 {
		t.Fatalf("cooldown did not expire: %s", retry)
	}
	limiter.succeeded("key")
	if retry := limiter.retryAfter("key"); retry != 0 {
		t.Fatalf("success did not clear limiter: %s", retry)
	}
}
