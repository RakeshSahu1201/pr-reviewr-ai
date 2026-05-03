// Package ratelimit provides a Redis-backed sliding-window rate limiter.
//
// The algorithm is implemented as an atomic Lua script executed on the Redis
// server to prevent race conditions during concurrent requests.  The caller
// receives a typed Result so it can set proper Retry-After / X-RateLimit-*
// headers.
//
// Failure modes
//
//   - Auth limiters (login / register):  fail-CLOSED.  If Redis is
//     unreachable the request is rejected to prevent brute-force storms.
//   - API limiters (authenticated routes): fail-OPEN.  If Redis is
//     unreachable the request is allowed through so the service stays up.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// ────────────────────────────────────────────────────────────────────────────
// Key taxonomy
// ────────────────────────────────────────────────────────────────────────────

const (
	authKeyPrefix = "ratelimit:auth:"   // ratelimit:auth:{ip}
	apiKeyPrefix  = "ratelimit:api:"    // ratelimit:api:{userID|ip}
)

func authKey(ip string) string   { return authKeyPrefix + ip }
func apiKey(id string) string    { return apiKeyPrefix + id }

// ────────────────────────────────────────────────────────────────────────────
// Lua script — sliding window counter (atomically consistent)
//
// KEYS[1]   – the rate-limit bucket key
// ARGV[1]   – current Unix time in milliseconds (string)
// ARGV[2]   – window size in milliseconds (string)
// ARGV[3]   – maximum allowed requests within the window (string)
//
// Returns: { current_count, allowed }
//          allowed == 1  → request permitted
//          allowed == 0  → request denied
// ────────────────────────────────────────────────────────────────────────────

var slidingWindowScript = redis.NewScript(`
local key      = KEYS[1]
local now      = tonumber(ARGV[1])
local window   = tonumber(ARGV[2])
local limit    = tonumber(ARGV[3])

-- Remove entries outside the current window.
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)

-- Count surviving entries.
local count = redis.call('ZCARD', key)

if count < limit then
    -- Add current request with score = now (milliseconds).
    -- Member must be unique; append a counter to handle same-millisecond requests.
    redis.call('ZADD', key, now, now .. '-' .. count)
    redis.call('PEXPIRE', key, window)
    return {count + 1, 1}
end

return {count, 0}
`)

// ────────────────────────────────────────────────────────────────────────────
// Result
// ────────────────────────────────────────────────────────────────────────────

// Result is returned by every Allow* call.
type Result struct {
	Allowed    bool
	Count      int64         // current request count in window
	Limit      int64         // configured limit
	RetryAfter time.Duration // how long until the window resets (0 if allowed)
}

// ────────────────────────────────────────────────────────────────────────────
// Limiter
// ────────────────────────────────────────────────────────────────────────────

// Limiter wraps a Redis client and exposes typed Allow helpers.
type Limiter struct {
	rdb *redis.Client
}

// New creates a Limiter.
func New(rdb *redis.Client) *Limiter {
	return &Limiter{rdb: rdb}
}

// allow executes the sliding-window Lua script and builds a Result.
func (l *Limiter) allow(
	ctx context.Context,
	key string,
	limit int64,
	window time.Duration,
	failOpen bool,
) (Result, error) {
	nowMs := time.Now().UnixMilli()
	windowMs := window.Milliseconds()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	vals, err := slidingWindowScript.Run(
		ctx, l.rdb,
		[]string{key},
		nowMs, windowMs, limit,
	).Int64Slice()

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isConnErr(err) {
			if failOpen {
				// Redis down — let the request through (API routes).
				return Result{Allowed: true, Count: 0, Limit: limit}, nil
			}
			// Redis down — block the request (auth routes).
			return Result{Allowed: false, Count: limit, Limit: limit,
				RetryAfter: window}, fmt.Errorf("ratelimit: redis unavailable: %w", err)
		}
		return Result{}, fmt.Errorf("ratelimit: script error: %w", err)
	}

	count := vals[0]
	allowed := vals[1] == 1

	var retryAfter time.Duration
	if !allowed {
		// Approximate: one full window gives a safe upper-bound.
		retryAfter = time.Duration(math.Ceil(float64(windowMs)/1000)) * time.Second
	}

	return Result{
		Allowed:    allowed,
		Count:      count,
		Limit:      limit,
		RetryAfter: retryAfter,
	}, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Public helpers
// ────────────────────────────────────────────────────────────────────────────

// AllowAuthIP enforces IP-based limits on auth routes (/login, /register).
// Default: 5 requests per 15 minutes.  Fails CLOSED on Redis error.
func (l *Limiter) AllowAuthIP(ctx context.Context, ip string) (Result, error) {
	return l.allow(ctx, authKey(ip), 5, 15*time.Minute, false)
}

// AllowAPI enforces per-user (authenticated) or per-IP (unauthenticated) limits
// on general API routes.  Default: 100 requests per minute.  Fails OPEN on Redis error.
func (l *Limiter) AllowAPI(ctx context.Context, identifier string) (Result, error) {
	return l.allow(ctx, apiKey(identifier), 100, time.Minute, true)
}

// ────────────────────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────────────────────

// isConnErr returns true for Redis connection / network errors.
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
