package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type bucketConfig struct {
	daily  int
	minute int
}

// bucketLimits must contain an entry for every rate_limit_bucket the Core
// Platform can mint (see core/internal/auth/tokens.go). A request whose bucket
// is missing here is rejected as "unknown bucket", so keep these in sync.
//
// Windowing is fixed-window per minute and per day (keys truncate to the window
// boundary via checkWindow); this is documented in proxy/docs/API.md.
var bucketLimits = map[string]bucketConfig{
	"free_standard":  {daily: 50, minute: 5},
	"pro_standard":   {daily: 1000, minute: 10},
	"power_standard": {daily: 5000, minute: 60},
	"admin":          {daily: 10000, minute: 100},
}

type Limiter struct {
	rdb        *redis.Client
	log        zerolog.Logger
	failClosed bool
}

type CheckResult struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

func New(rdb *redis.Client, log zerolog.Logger, failClosed bool) *Limiter {
	return &Limiter{rdb: rdb, log: log, failClosed: failClosed}
}

type LimitError struct {
	RetryAfter time.Duration
}

func (e *LimitError) Error() string {
	return "rate limit exceeded"
}

func (l *Limiter) Check(ctx context.Context, userSub, bucket string) (*CheckResult, error) {
	cfg, ok := bucketLimits[bucket]
	if !ok {
		return nil, fmt.Errorf("unknown rate limit bucket: %s", bucket)
	}
	now := time.Now()

	if cfg.minute > 0 {
		if err := l.checkWindow(ctx, fmt.Sprintf("proxy:rl:min:%s:%s", bucket, userSub), cfg.minute, time.Minute, now); err != nil {
			return nil, err
		}
	}
	if cfg.daily > 0 {
		if err := l.checkWindow(ctx, fmt.Sprintf("proxy:rl:day:%s:%s", bucket, userSub), cfg.daily, 24*time.Hour, now); err != nil {
			return nil, err
		}
	}

	limit := cfg.minute
	if limit == 0 {
		limit = cfg.daily
	}
	window := time.Minute
	if cfg.minute == 0 {
		window = 24 * time.Hour
	}
	key := fmt.Sprintf("proxy:rl:min:%s:%s", bucket, userSub)
	if cfg.minute == 0 {
		key = fmt.Sprintf("proxy:rl:day:%s:%s", bucket, userSub)
	}
	count, err := l.rdb.Get(ctx, key).Int64()
	if err != nil && err != redis.Nil {
		if l.failClosed {
			return nil, fmt.Errorf("rate limit unavailable")
		}
		count = 0
	}
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	reset := now.Truncate(window).Add(window)
	return &CheckResult{
		Limit:     limit,
		Remaining: remaining,
		Reset:     reset,
	}, nil
}

func (l *Limiter) checkWindow(ctx context.Context, key string, limit int, window time.Duration, now time.Time) error {
	windowStart := now.Truncate(window)

	pipe := l.rdb.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.ExpireAt(ctx, key, windowStart.Add(window))
	if _, err := pipe.Exec(ctx); err != nil {
		l.log.Warn().Err(err).Str("key", key).Msg("rate limit redis error")
		if l.failClosed {
			return fmt.Errorf("rate limit unavailable")
		}
		return nil
	}
	count, err := incr.Result()
	if err != nil {
		if l.failClosed {
			return fmt.Errorf("rate limit unavailable")
		}
		return nil
	}
	if int(count) > limit {
		return &LimitError{RetryAfter: windowStart.Add(window).Sub(now)}
	}
	return nil
}

func (l *Limiter) Ping(ctx context.Context) error {
	return l.rdb.Ping(ctx).Err()
}
