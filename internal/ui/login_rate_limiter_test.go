// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"testing"
	"time"
)

func TestLoginRateLimiterRecordFailedAttempt(t *testing.T) {
	limiter := newLoginRateLimiter()
	defer limiter.Close()

	// First 5 attempts should not be rate limited
	for i := range 5 {
		isLimited, retryAfter := limiter.recordFailedAttempt("1.2.3.4")
		if isLimited {
			t.Fatalf("Attempt %d should not be rate limited", i+1)
		}
		if retryAfter != 0 {
			t.Fatalf("Attempt %d should return 0 retryAfter", i+1)
		}
	}

	// 6th attempt should be rate limited
	isLimited, retryAfter := limiter.recordFailedAttempt("1.2.3.4")
	if !isLimited {
		t.Error("6th attempt should be rate limited")
	}
	// retryAfter is the raw remaining window duration. After recording the
	// 6th failure (which sets lastAt=now), the remaining window is ~loginWindow.
	if retryAfter <= 0 || retryAfter > loginWindow {
		t.Errorf("6th attempt retryAfter should be within (0, loginWindow], got %v", retryAfter)
	}

	// Different IP should not be rate limited
	isLimited, retryAfter = limiter.recordFailedAttempt("5.6.7.8")
	if isLimited {
		t.Error("Different IP should not be rate limited")
	}
	if retryAfter != 0 {
		t.Error("Different IP should return 0 retryAfter")
	}
}

func TestLoginRateLimiterIsRateLimited(t *testing.T) {
	limiter := newLoginRateLimiter()
	defer limiter.Close()

	// Initially not limited
	if limiter.isRateLimited("1.2.3.4") {
		t.Error("Should not be rate limited initially")
	}

	// Make 5 attempts
	for range 5 {
		limiter.recordFailedAttempt("1.2.3.4")
	}

	// Now should be rate limited (without recording a new attempt)
	if !limiter.isRateLimited("1.2.3.4") {
		t.Error("Should be rate limited after 5 attempts")
	}

	// recordFailedAttempt on an already-limited IP should still return true
	isLimited, _ := limiter.recordFailedAttempt("1.2.3.4")
	if !isLimited {
		t.Error("Should return true on already-limited IP")
	}

	// Reset should clear the limit
	limiter.reset("1.2.3.4")
	if limiter.isRateLimited("1.2.3.4") {
		t.Error("Should not be rate limited after reset")
	}
}

func TestLoginRateLimiterWindowExpiration(t *testing.T) {
	limiter := newLoginRateLimiter()
	defer limiter.Close()

	// Make 5 attempts
	for range 5 {
		limiter.recordFailedAttempt("1.2.3.4")
	}

	// 6th attempt is rate limited
	isLimited, retryAfter := limiter.recordFailedAttempt("1.2.3.4")
	if !isLimited {
		t.Error("Should be rate limited after 5 attempts")
	}
	if retryAfter == 0 {
		t.Error("Should return non-zero retryAfter")
	}

	// Simulate window expiration by creating a manual entry
	limiter.mu.Lock()
	entry := &attemptEntry{
		count:  5,
		lastAt: time.Now().Add(-2 * loginWindow),
	}
	limiter.attempts["1.2.3.4-reset"] = entry
	limiter.mu.Unlock()

	// New attempt after window should start fresh
	isLimited, retryAfter = limiter.recordFailedAttempt("1.2.3.4-reset")
	if isLimited {
		t.Error("Should not be rate limited after window expires")
	}
	if retryAfter != 0 {
		t.Error("Should return 0 retryAfter after window expires")
	}
	limiter.mu.Lock()
	newEntry := limiter.attempts["1.2.3.4-reset"]
	limiter.mu.Unlock()
	if newEntry.count != 1 {
		t.Errorf("Expected count 1 after window expiry, got %d", newEntry.count)
	}
}

func TestLoginRateLimiterCleanup(t *testing.T) {
	limiter := newLoginRateLimiter()
	defer limiter.Close()

	// Add an old entry manually
	limiter.mu.Lock()
	limiter.attempts["old-ip"] = &attemptEntry{
		count:  5,
		lastAt: time.Now().Add(-15 * loginWindow),
	}
	limiter.mu.Unlock()

	// Cleanup should remove it
	limiter.cleanup()

	limiter.mu.Lock()
	_, exists := limiter.attempts["old-ip"]
	limiter.mu.Unlock()
	if exists {
		t.Error("Old entry should have been cleaned up")
	}

	// Add a recent entry - should NOT be cleaned up
	limiter.mu.Lock()
	limiter.attempts["new-ip"] = &attemptEntry{
		count:  1,
		lastAt: time.Now(),
	}
	limiter.mu.Unlock()

	limiter.cleanup()

	limiter.mu.Lock()
	_, exists = limiter.attempts["new-ip"]
	limiter.mu.Unlock()
	if !exists {
		t.Error("Recent entry should not have been cleaned up")
	}
}

func TestLoginRateLimiterClose(t *testing.T) {
	limiter := newLoginRateLimiter()

	// Close should be safe to call multiple times
	limiter.Close()
	limiter.Close()
	limiter.Close()

	// Subsequent operations should not panic
	limiter.reset("1.2.3.4")
	limiter.isRateLimited("1.2.3.4")
}

func TestLoginRateLimiterRetryAfter(t *testing.T) {
	limiter := newLoginRateLimiter()
	defer limiter.Close()

	// No entry yet
	d := limiter.retryAfter("1.2.3.4")
	if d != 0 {
		t.Errorf("retryAfter for unknown IP should be 0, got %v", d)
	}

	// Make 5 attempts (not yet rate limited)
	for range 5 {
		limiter.recordFailedAttempt("1.2.3.4")
	}

	// retryAfter for an active entry returns the remaining window time
	// (not zero, even though the limit is not yet triggered).
	d = limiter.retryAfter("1.2.3.4")
	if d == 0 {
		t.Error("retryAfter for IP with active entry should be non-zero")
	}

	// 6th attempt triggers rate limit
	limiter.recordFailedAttempt("1.2.3.4")

	d = limiter.retryAfter("1.2.3.4")
	if d == 0 {
		t.Error("retryAfter should still return remaining time after rate limited")
	}
}
