package ratelimit_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/yadra-team/yadra-bridge/internal/ratelimit"
)

func TestLimiterAllowsUnderMinuteCap(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	log := zerolog.Nop()
	l := ratelimit.New(rdb, log, false)

	for i := 0; i < 10; i++ {
		if _, err := l.Check(context.Background(), "user-1", "pro_standard"); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
}

func TestLimiterBlocksOverMinuteCap(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	log := zerolog.Nop()
	l := ratelimit.New(rdb, log, false)

	for i := 0; i < 10; i++ {
		_, _ = l.Check(context.Background(), "user-2", "pro_standard")
	}
	if _, err := l.Check(context.Background(), "user-2", "pro_standard"); err == nil {
		t.Fatal("expected minute rate limit error")
	}
}

func TestLimiterReturnsHeaders(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	l := ratelimit.New(rdb, zerolog.Nop(), false)
	res, err := l.Check(context.Background(), "user-3", "pro_standard")
	if err != nil {
		t.Fatal(err)
	}
	if res.Limit != 10 || res.Remaining < 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestLimiterKnownBuckets(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	l := ratelimit.New(rdb, zerolog.Nop(), false)
	for _, bucket := range []string{"free_standard", "pro_standard", "power_standard", "admin"} {
		if _, err := l.Check(context.Background(), "user-"+bucket, bucket); err != nil {
			t.Fatalf("bucket %q should be known: %v", bucket, err)
		}
	}
}

func TestLimiterUnknownBucketRejected(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	l := ratelimit.New(rdb, zerolog.Nop(), false)
	if _, err := l.Check(context.Background(), "user-x", "does_not_exist"); err == nil {
		t.Fatal("expected error for unknown bucket")
	}
}

func TestLimiterFailClosedOnRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mr.Close()
	l := ratelimit.New(rdb, zerolog.Nop(), true)
	if _, err := l.Check(context.Background(), "user-y", "pro_standard"); err == nil {
		t.Fatal("expected fail-closed error when Redis is down")
	}
}
