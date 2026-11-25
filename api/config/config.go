package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	deliveryBatchMaxDefault  = 64
	deliveryBatchMaxLimit    = 256
	deliveryBatchWaitDefault = time.Millisecond
	deliveryBatchWaitMin     = 100 * time.Microsecond
	deliveryBatchWaitMax     = 100 * time.Millisecond
)

type Config struct {
	Port                 string
	DBDSN                string
	DBMaxConns           int32
	DBMinConns           int32
	DeliveryBatchEnabled bool
	DeliveryBatchMax     int
	DeliveryBatchWait    time.Duration
	RedisAddr            string
	RedisPassword        string
	RateLimit            int
	RateWindow           int    // seconds
	SMTPServerIP         string // SMTP_SERVER_IP 环境变量，仅在后台未设置 smtp_server_ip 时作为回退
	SMTPHostname         string // 邮件服务器场指向的 hostname，不硬编码
	LinuxDOClientID      string
	LinuxDOClientSecret  string
	LinuxDORedirectURL   string
	GitHubClientID       string
	GitHubClientSecret   string
	GitHubRedirectURL    string
	// InternalAPIKey 保护 /internal/* 接口（X-Internal-Key 头）。
	// 为空时 /internal/* 仅允许回环/私有网段的直连对端访问。
	InternalAPIKey string
	// TrustedProxies 允许其 X-Forwarded-For 被信任的代理 IP/CIDR（TRUSTED_PROXIES，逗号分隔）。
	// 默认仅回环与私有网段（Docker 内网的 nginx）。前面有 CDN（如 Cloudflare）时，
	// 需把 CDN 回源网段加进来，否则所有访客会被识别成 CDN 节点 IP 共用一个限流桶。
	TrustedProxies []string
	// TrustedPlatform 直接从该请求头读取客户端 IP（TRUSTED_PLATFORM）。
	// 可填 "cloudflare"（=CF-Connecting-IP）或任意头名；为空则不启用。
	TrustedPlatform string
}

var defaultTrustedProxies = []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

func parseTrustedProxies(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultTrustedProxies...)
	}
	return out
}

func parseTrustedPlatform(v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "":
		return ""
	case "cloudflare", "cf":
		return "CF-Connecting-IP"
	case "google", "gae", "appengine":
		return "X-Appengine-Remote-Addr"
	case "flyio", "fly":
		return "Fly-Client-IP"
	}
	return v
}

func Load() *Config {
	rl, _ := strconv.Atoi(getEnv("RATE_LIMIT", "500"))
	rw, _ := strconv.Atoi(getEnv("RATE_WINDOW", "60"))
	dbMaxConns := getEnvInt32("DB_MAX_CONNS", 128)
	dbMinConns := getEnvInt32("DB_MIN_CONNS", 16)
	if dbMinConns > dbMaxConns {
		dbMinConns = dbMaxConns
	}
	deliveryBatchMax := getEnvInt("DELIVERY_BATCH_MAX", deliveryBatchMaxDefault)
	if deliveryBatchMax > deliveryBatchMaxLimit {
		deliveryBatchMax = deliveryBatchMaxDefault
	}
	deliveryBatchWait := getEnvDuration("DELIVERY_BATCH_WAIT", deliveryBatchWaitDefault)
	if deliveryBatchWait < deliveryBatchWaitMin || deliveryBatchWait > deliveryBatchWaitMax {
		deliveryBatchWait = deliveryBatchWaitDefault
	}

	return &Config{
		// PORT：API 容器内监听端口，默认 8080。
		// 由 .env 中的 API_PORT 注入。修改此端口后需同步：
		//   1. .env / .env.example 的 API_PORT
		//   2. docker-compose.yml api.ports 右边数字
		//   3. nginx/default.conf 所有 proxy_pass http://api:8080
		//   4. postfix/entrypoint.sh API_URL 默认值
		Port:                 getEnv("PORT", "8080"),
		DBDSN:                getEnv("DB_DSN", ""),
		DBMaxConns:           dbMaxConns,
		DBMinConns:           dbMinConns,
		DeliveryBatchEnabled: getEnvBool("DELIVERY_BATCH_ENABLED", true),
		DeliveryBatchMax:     deliveryBatchMax,
		DeliveryBatchWait:    deliveryBatchWait,
		// RedisAddr：Redis 容器内部地址，格式 "host:port"。
		// 默认 "redis:6379"，"redis" 是 Docker 内部服务名，不需要修改。
		RedisAddr:           getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword:       getEnv("REDIS_PASSWORD", ""),
		RateLimit:           rl,
		RateWindow:          rw,
		SMTPServerIP:        os.Getenv("SMTP_SERVER_IP"),
		SMTPHostname:        os.Getenv("SMTP_HOSTNAME"),
		LinuxDOClientID:     os.Getenv("LINUXDO_CLIENT_ID"),
		LinuxDOClientSecret: os.Getenv("LINUXDO_CLIENT_SECRET"),
		LinuxDORedirectURL:  os.Getenv("LINUXDO_REDIRECT_URL"),
		GitHubClientID:      os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
		GitHubRedirectURL:   os.Getenv("GITHUB_REDIRECT_URL"),
		InternalAPIKey:      os.Getenv("INTERNAL_API_KEY"),
		TrustedProxies:      parseTrustedProxies(os.Getenv("TRUSTED_PROXIES")),
		TrustedPlatform:     parseTrustedPlatform(os.Getenv("TRUSTED_PLATFORM")),
	}
}

func getEnvBool(key string, fallback bool) bool {
	v, err := strconv.ParseBool(getEnv(key, strconv.FormatBool(fallback)))
	if err != nil {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	v, err := strconv.Atoi(getEnv(key, strconv.Itoa(fallback)))
	if err != nil || v < 1 {
		return fallback
	}
	return v
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(getEnv(key, fallback.String()))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func getEnvInt32(key string, fallback int32) int32 {
	v, err := strconv.ParseInt(getEnv(key, strconv.FormatInt(int64(fallback), 10)), 10, 32)
	if err != nil || v < 1 {
		return fallback
	}
	return int32(v)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
