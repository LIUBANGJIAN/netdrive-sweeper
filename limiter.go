package main

import (
	"context"

	"golang.org/x/time/rate"
)

// Limiter 是对标准令牌桶的薄封装，全局共享、支持取消。
type Limiter struct {
	lim *rate.Limiter
}

func newLimiter(opsPerSec float64, burst int) *Limiter {
	if opsPerSec <= 0 {
		opsPerSec = 5.0
	}
	if burst <= 0 {
		burst = 1
	}
	return &Limiter{lim: rate.NewLimiter(rate.Limit(opsPerSec), burst)}
}

// Wait 阻塞直到取得一个令牌或 ctx 取消。所有 CD2 gRPC 调用都必须先过这里。
func (l *Limiter) Wait(ctx context.Context) error {
	return l.lim.Wait(ctx)
}

// SetRate 动态调整速率与突发。
func (l *Limiter) SetRate(opsPerSec float64, burst int) {
	if opsPerSec <= 0 {
		opsPerSec = 5.0
	}
	if burst <= 0 {
		burst = 1
	}
	l.lim.SetLimit(rate.Limit(opsPerSec))
	l.lim.SetBurst(burst)
}
