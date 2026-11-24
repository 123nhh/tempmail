package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// fixedWindowScript 原子检查并递增固定窗口计数。
// 仅在窗口首次创建（或键意外缺少 TTL）时设置 PEXPIRE，
// 避免持续低频请求或 429 重试无限延长窗口导致永久锁死。
var fixedWindowScript = redis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local ttl = redis.call("PTTL", KEYS[1])
local limit = tonumber(ARGV[2])
local window_ms = tonumber(ARGV[1])
if current >= limit then
  if ttl < 0 then
    redis.call("PEXPIRE", KEYS[1], window_ms)
    ttl = window_ms
  end
  return {current + 1, ttl}
end
current = redis.call("INCR", KEYS[1])
if current == 1 or ttl < 0 then
  redis.call("PEXPIRE", KEYS[1], window_ms)
  ttl = window_ms
end
return {current, ttl}
`)

// RateLimit 基于 Redis Lua 原子固定窗口的速率限制（认证后按账户 ID 建桶，否则按哈希 IP）
// limit: 每个窗口允许的请求数
// window: 窗口大小（秒）
func RateLimit(rdb *redis.Client, limit int, window int) gin.HandlerFunc {
	return rateLimitWithKey(rdb, limit, window, func(c *gin.Context) string {
		if account := GetAccount(c); account != nil {
			return "rl:acct:" + account.ID.String()
		}
		return "rl:ip:" + hashKey(c.ClientIP())
	})
}

// RateLimitByIP 按哈希客户端 IP 与作用域限流（用于公开登录/注册及认证前防刷）
func RateLimitByIP(rdb *redis.Client, scope string, limit int, window int) gin.HandlerFunc {
	prefix := "rl:ip:" + scope + ":"
	return rateLimitWithKey(rdb, limit, window, func(c *gin.Context) string {
		return prefix + hashKey(c.ClientIP())
	})
}

func rateLimitWithKey(rdb *redis.Client, limit int, window int, keyFn func(*gin.Context) string) gin.HandlerFunc {
	if rdb == nil || limit <= 0 || window <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	windowMs := int64(window) * 1000

	return func(c *gin.Context) {
		redisKey := keyFn(c)
		ctx := c.Request.Context()

		vals, err := fixedWindowScript.Run(ctx, rdb, []string{redisKey}, windowMs, limit).Int64Slice()
		if err != nil || len(vals) < 2 {
			// Redis 故障时放行（fail-open）
			c.Next()
			return
		}

		count := vals[0]
		ttlMs := vals[1]
		if ttlMs < 0 {
			ttlMs = windowMs
		}

		remaining := int64(limit) - count
		if remaining < 0 {
			remaining = 0
		}
		retryAfter := int((ttlMs + 999) / 1000)
		if retryAfter < 1 {
			retryAfter = 1
		}
		resetAt := time.Now().Add(time.Duration(ttlMs) * time.Millisecond).Unix()

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

		if count > int64(limit) {
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"limit":       limit,
				"retry_after": retryAfter,
			})
			return
		}

		c.Next()
	}
}

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}
