// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"sync"
	"time"
)

// Login rate limit configuration.
const (
	loginMaxAttempts   = 5
	loginWindow        = time.Minute
	cleanupInterval    = 5 * time.Minute
	entryTTL           = 10 * loginWindow
	loginRateLimitCode = 429
)

// loginRateLimiter tracks failed login attempts per client IP using a rolling
// (stop-the-clock) window. Each failed attempt resets the window start time;
// the limit triggers when loginMaxAttempts failures occur without a gap
// exceeding loginWindow. It is single-instance only — in multi-instance
// deployments the effective threshold is multiplied by the number of replicas.
//
// Security note: this limiter keys on the client IP (resolving X-Forwarded-For
// when behind a trusted proxy). An attacker with a proxy pool can bypass by
// rotating IPs. For stronger protection, consider also rate-limiting per
// username at a load balancer or reverse proxy.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptEntry
	stopCh   chan struct{} // signals cleanup goroutine to stop
}

type attemptEntry struct {
	count  int
	lastAt time.Time
}

// newLoginRateLimiter creates a rate limiter with cleanup goroutine.
// The caller must call Close() to release the goroutine.
func newLoginRateLimiter() *loginRateLimiter {
	l := &loginRateLimiter{
		attempts: make(map[string]*attemptEntry),
		stopCh:   make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Close stops the cleanup goroutine.
func (l *loginRateLimiter) Close() {
	select {
	case <-l.stopCh:
		return // already closed
	default:
	}
	close(l.stopCh)
}

// cleanupLoop removes expired entries on a periodic basis.
func (l *loginRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.stopCh:
			return
		}
	}
}

// cleanup removes entries older than entryTTL.
func (l *loginRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for ip, entry := range l.attempts {
		if now.Sub(entry.lastAt) > entryTTL {
			delete(l.attempts, ip)
		}
	}
}

// isRateLimited checks if the client is rate limited without recording a new attempt.
func (l *loginRateLimiter) isRateLimited(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	entry, exists := l.attempts[ip]
	if !exists {
		return false
	}

	if now.Sub(entry.lastAt) > loginWindow {
		return false
	}

	return entry.count >= loginMaxAttempts
}

// recordFailedAttempt records a failed login attempt for the given IP and
// returns (isRateLimited, retryAfter). retryAfter is the raw remaining window
// duration (0 if not yet rate limited, or 0 in the unreachable case where the
// computed duration is negative — the clamp is defensive). Callers emitting
// a Retry-After header should apply any ceiling adjustment (e.g. +1s) at the
// HTTP boundary.
//
// Under the hood: the first loginMaxAttempts calls return (false, 0).
// The loginMaxAttempts+1 call records the overflow and returns (true, duration).
func (l *loginRateLimiter) recordFailedAttempt(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	entry, exists := l.attempts[ip]

	if !exists || now.Sub(entry.lastAt) > loginWindow {
		// Start a new window
		l.attempts[ip] = &attemptEntry{
			count:  1,
			lastAt: now,
		}
		return false, 0
	}

	// Increment within the same window
	entry.count++
	entry.lastAt = now

	if entry.count > loginMaxAttempts {
		// Return the raw remaining window duration. Callers are responsible
		// for any ceiling adjustment (e.g. +1s) when emitting Retry-After.
		retryAfter := time.Until(entry.lastAt.Add(loginWindow))
		if retryAfter < 0 {
			retryAfter = 0
		}
		return true, retryAfter
	}

	return false, 0
}

// retryAfter returns the remaining window duration for the given IP, or 0
// if there is no active entry or the window has expired.
func (l *loginRateLimiter) retryAfter(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.attempts[ip]
	if !exists {
		return 0
	}

	retryAfter := time.Until(entry.lastAt.Add(loginWindow))
	if retryAfter < 0 {
		return 0
	}
	return retryAfter
}

// reset clears the rate limiter state for a given IP.
func (l *loginRateLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}
