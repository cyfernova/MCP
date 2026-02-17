package middleware

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type userLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	mu sync.Mutex

	limit rate.Limit
	burst int

	limiters map[string]*userLimiter

	idleTTL     time.Duration
	lastCleanup time.Time
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	return &RateLimiter{
		limit:       rate.Limit(rps),
		burst:       burst,
		limiters:    make(map[string]*userLimiter),
		idleTTL:     30 * time.Minute,
		lastCleanup: time.Now(),
	}
}

func (r *RateLimiter) Allow(userID string) bool {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	if now.Sub(r.lastCleanup) > 5*time.Minute {
		r.cleanupLocked(now)
		r.lastCleanup = now
	}

	entry, ok := r.limiters[userID]
	if !ok {
		entry = &userLimiter{
			limiter:  rate.NewLimiter(r.limit, r.burst),
			lastSeen: now,
		}
		r.limiters[userID] = entry
	}

	entry.lastSeen = now
	return entry.limiter.Allow()
}

func (r *RateLimiter) cleanupLocked(now time.Time) {
	for userID, limiter := range r.limiters {
		if now.Sub(limiter.lastSeen) > r.idleTTL {
			delete(r.limiters, userID)
		}
	}
}
