package auth

import (
	"sync"
	"time"
)

const loginFailureWindow = 10 * time.Minute

type loginAttempt struct {
	windowStarted time.Time
	failures      int
	blockedUntil  time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	now      func() time.Time
	attempts map[string]loginAttempt
}

func newLoginLimiter(now func() time.Time) *loginLimiter {
	return &loginLimiter{now: now, attempts: make(map[string]loginAttempt)}
}

func (l *loginLimiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	attempt, ok := l.attempts[key]
	if !ok {
		return 0
	}
	if now.Sub(attempt.windowStarted) >= loginFailureWindow && !attempt.blockedUntil.After(now) {
		delete(l.attempts, key)
		return 0
	}
	if attempt.blockedUntil.After(now) {
		return attempt.blockedUntil.Sub(now)
	}
	return 0
}

func (l *loginLimiter) failed(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	attempt, ok := l.attempts[key]
	if !ok || now.Sub(attempt.windowStarted) >= loginFailureWindow {
		attempt = loginAttempt{windowStarted: now}
	}
	attempt.failures++
	if attempt.failures >= 5 {
		shift := attempt.failures - 5
		if shift > 5 {
			shift = 5
		}
		cooldown := 30 * time.Second * time.Duration(1<<shift)
		if cooldown > 15*time.Minute {
			cooldown = 15 * time.Minute
		}
		attempt.blockedUntil = now.Add(cooldown)
	}
	l.attempts[key] = attempt
	if attempt.blockedUntil.After(now) {
		return attempt.blockedUntil.Sub(now)
	}
	return 0
}

func (l *loginLimiter) succeeded(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}
