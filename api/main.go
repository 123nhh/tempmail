package main

import (
	"context"
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"tempmail/config"
	"tempmail/handler"
	"tempmail/middleware"
	"tempmail/model"
	"tempmail/realtime"
	"tempmail/store"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	// ==================== 连接数据库 ====================
	ctx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	db, err := store.NewWithOptions(ctx, cfg.DBDSN, store.PoolOptions{
		MaxConns: cfg.DBMaxConns,
		MinConns: cfg.DBMinConns,
	})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()
	log.Println("[OK] Database connected")
	// 幂等补齐新版本新增的列（老库升级用，init.sql 仅在首次初始化时执行）
	if err := db.EnsureSchema(ctx); err != nil {
		log.Printf("[WARN] ensure schema failed: %v", err)
	}
	var deliveryBatcher *store.DeliveryBatcher
	if cfg.DeliveryBatchEnabled {
		deliveryBatcher = store.NewDeliveryBatcher(ctx, db, cfg.DeliveryBatchMax, cfg.DeliveryBatchWait)
		log.Printf("[OK] Delivery micro-batcher started (max=%d, wait=%s)", cfg.DeliveryBatchMax, cfg.DeliveryBatchWait)
	} else {
		log.Println("[OK] Delivery micro-batcher disabled")
	}

	// ==================== 连接 Redis ====================
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           0,
		PoolSize:     0, // 0 = 不限（自动按 CPU 核心数 * 10）
		MinIdleConns: 20,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect redis: %v", err)
	}
	defer rdb.Close()

	// 新邮件实时推送（SSE），多实例部署时通过 Redis Pub/Sub 共享事件
	var bgWG sync.WaitGroup
	hub := realtime.NewHub(rdb)
	bgWG.Add(1)
	go func() {
		defer bgWG.Done()
		hub.Run(ctx)
	}()
	publishDelivered := func(c *gin.Context, d store.DeliveredEmail, sender, subject string, size int) {
		if !d.Delivered {
			return
		}
		hub.Publish(c.Request.Context(), d.MailboxID, "email", model.EmailSummary{
			ID: d.EmailID, Sender: sender, Subject: subject, SizeBytes: size, ReceivedAt: time.Now().UTC(),
		})
	}
	log.Println("[OK] Redis connected")

	// ==================== Gin 路由 ====================
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipPaths: []string{"/health"}}))
	if err := r.SetTrustedProxies([]string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}); err != nil {
		log.Printf("[WARN] failed to set trusted proxies: %v", err)
	}

	// CORS：允许前端跨域访问
	r.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"*"},
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization", "X-API-Key"},
		ExposeHeaders: []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "Retry-After"},
		MaxAge:        12 * time.Hour,
	}))

	// 健康检查（无需认证）
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().Unix()})
	})

	// 初始化 handlers
	accountH := handler.NewAccountHandler(db)
	domainH := handler.NewDomainHandler(db, cfg.SMTPServerIP, cfg.SMTPHostname)
	mailboxH := handler.NewMailboxHandler(db)
	liveH := handler.NewLiveHandler(db, hub)
	emailH := handler.NewEmailHandler(db)
	settingH := handler.NewSettingHandler(db)
	registerH := handler.NewRegisterHandler(db)
	statsH := handler.NewStatsHandler(db)
	linuxDOH := handler.NewLinuxDOHandler(db, cfg.LinuxDOClientID, cfg.LinuxDOClientSecret, cfg.LinuxDORedirectURL)
	gitHubH := handler.NewGitHubHandler(db, cfg.GitHubClientID, cfg.GitHubClientSecret, cfg.GitHubRedirectURL)

	// 公开路由（无需认证）
	public := r.Group("/public")
	{
		public.GET("/settings", settingH.GetPublic)
		public.POST("/key-login", middleware.RateLimitByIP(rdb, "key-login", 30, 60), accountH.KeyLogin)
		public.POST("/register", middleware.RateLimitByIP(rdb, "register", 20, 60), registerH.Register)
		public.GET("/stats", statsH.Get)
		public.GET("/auth/linuxdo", linuxDOH.Start)
		public.GET("/auth/linuxdo/callback", linuxDOH.Callback)
		public.GET("/auth/github", gitHubH.Start)
		public.GET("/auth/github/callback", gitHubH.Callback)
	}

	// API 路由组（认证前 IP 防刷 + API Key 认证 + 账户级速率限制）
	preAuthLimit := cfg.RateLimit * 2
	if preAuthLimit < 1000 {
		preAuthLimit = 1000
	}
	api := r.Group("/api")
	api.Use(middleware.RateLimitByIP(rdb, "api-preauth", preAuthLimit, cfg.RateWindow))
	api.Use(middleware.Auth(db))
	api.Use(middleware.RateLimit(rdb, cfg.RateLimit, cfg.RateWindow))
	{
		// 当前用户
		api.GET("/me", accountH.Me)

		// 域名池（所有用户可查看）
		api.GET("/domains", domainH.List)
		api.GET("/domains/:id/status", domainH.GetStatus) // 任意用户可轮询域名状态
		api.GET("/stats", statsH.Get)
		// 任意已登录用户可提交域名进行 MX 自动验证
		api.POST("/domains/submit", domainH.Submit)

		// 邮箱管理
		api.POST("/mailboxes", mailboxH.Create)
		api.GET("/mailboxes", mailboxH.List)
		api.GET("/mailboxes/:id", mailboxH.Get)
		api.DELETE("/mailboxes/:id", mailboxH.Delete)
		api.POST("/mailboxes/:id/extend", liveH.Extend)
		api.GET("/mailboxes/:id/events", liveH.Events)

		// 邮件管理
		api.GET("/emails/recent", emailH.Recent)
		api.GET("/mailboxes/:id/emails", emailH.List)
		api.GET("/mailboxes/:id/emails/:email_id", emailH.Get)
		api.DELETE("/mailboxes/:id/emails/:email_id", emailH.Delete)
		// 管理员路由
		admin := api.Group("/admin")
		admin.Use(middleware.AdminOnly())
		{
			admin.POST("/accounts", accountH.Create)
			admin.GET("/accounts", accountH.List)
			admin.DELETE("/accounts/:id", accountH.Delete)

			admin.POST("/domains", domainH.Add)
			admin.POST("/domains/mx-import", domainH.MXImport)
			admin.POST("/domains/mx-register", domainH.MXRegister)
			admin.POST("/domains/refresh-mx", domainH.RefreshMX)
			admin.DELETE("/domains/:id", domainH.Delete)
			admin.PUT("/domains/:id/toggle", domainH.Toggle)
			admin.GET("/domains/:id/status", domainH.GetStatus)

			// 系统设置管理
			admin.GET("/settings", settingH.AdminGetAll)
			admin.PUT("/settings", settingH.AdminUpdate)
		}
	}

	// 内部邮件投递接口（Postfix pipe 调用，仅内部网络）
	if cfg.InternalAPIKey == "" {
		log.Printf("[WARN] INTERNAL_API_KEY is not set; /internal/* is restricted to loopback/private peer IPs only")
	}
	internal := r.Group("/internal")
	internal.Use(internalAuthMiddleware(cfg.InternalAPIKey))
	{
		// 域名列表（供 Postfix 同步）
		internal.GET("/domains", func(c *gin.Context) {
			domains, err := db.ListDomains(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"domains": domains})
		})

		internal.POST("/deliver", maxBodySizeMiddleware(internalDeliverMaxBodyBytes), func(c *gin.Context) {
			var req struct {
				Recipient string `json:"recipient" binding:"required"`
				Sender    string `json:"sender"`
				Subject   string `json:"subject"`
				BodyText  string `json:"body_text"`
				BodyHTML  string `json:"body_html"`
				Raw       string `json:"raw"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			var delivery store.DeliveredEmail
			var err error
			if deliveryBatcher != nil {
				delivery, err = deliveryBatcher.Deliver(c.Request.Context(), store.MessageDelivery{
					Recipient: req.Recipient,
					Sender:    req.Sender,
					Subject:   req.Subject,
					BodyText:  req.BodyText,
					BodyHTML:  req.BodyHTML,
					Raw:       req.Raw,
				})
			} else {
				delivery, err = db.DeliverMessage(c.Request.Context(), store.MessageDelivery{
					Recipient: req.Recipient,
					Sender:    req.Sender,
					Subject:   req.Subject,
					BodyText:  req.BodyText,
					BodyHTML:  req.BodyHTML,
					Raw:       req.Raw,
				})
			}
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if !delivery.Delivered {
				// 邮箱不存在，静默丢弃。
				c.JSON(http.StatusOK, gin.H{"status": "discarded", "reason": "unknown recipient"})
				return
			}
			publishDelivered(c, delivery, req.Sender, req.Subject, len(req.Raw))
			c.JSON(http.StatusOK, gin.H{"status": "delivered", "email_id": delivery.EmailID})
		})

		// 批量投递：一次 HTTP 调用写入多个收件人。
		// 用于 LMTP 守护进程把同一会话的多个 RCPT 合并提交。
		// 任意一条失败会返回 5xx，调用方应当重试整批，避免丢邮件。
		internal.POST("/deliver-batch", maxBodySizeMiddleware(internalDeliverMaxBodyBytes), func(c *gin.Context) {
			var req struct {
				Recipients []string `json:"recipients" binding:"required,min=1"`
				Sender     string   `json:"sender"`
				Subject    string   `json:"subject"`
				BodyText   string   `json:"body_text"`
				BodyHTML   string   `json:"body_html"`
				Raw        string   `json:"raw"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			deliveries, err := db.DeliverEmails(c.Request.Context(), req.Recipients,
				req.Sender, req.Subject, req.BodyText, req.BodyHTML, req.Raw)
			if err != nil {
				// The set-based statement is atomic, so a statement-wide failure
				// cannot be attributed to one recipient.
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "recipient": ""})
				return
			}

			results := make([]gin.H, 0, len(deliveries))
			for _, delivery := range deliveries {
				if !delivery.Delivered {
					results = append(results, gin.H{"recipient": delivery.Recipient, "status": "discarded", "reason": "unknown recipient"})
					continue
				}
				publishDelivered(c, delivery, req.Sender, req.Subject, len(req.Raw))
				results = append(results, gin.H{"recipient": delivery.Recipient, "status": "delivered", "email_id": delivery.EmailID})
			}
			c.JSON(http.StatusOK, gin.H{"results": results})
		})
	}

	// ==================== 邮箱自动过期清理 ====================
	bgWG.Add(1)
	go func() {
		defer bgWG.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		log.Println("[OK] Mailbox expiry cleaner started (TTL=30min, interval=1min)")
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if deleted, err := db.DeleteExpiredMailboxes(ctx); err != nil {
					if ctx.Err() == nil {
						log.Printf("[cleaner] error: %v", err)
					}
				} else if deleted > 0 {
					log.Printf("[cleaner] deleted %d expired mailboxes", deleted)
				}
			}
		}
	}()

	// ==================== 邮件统计计数器后台 flush ====================
	statsFlusherDone := make(chan struct{})
	go func() {
		defer close(statsFlusherDone)
		db.RunStatsFlusher(ctx, time.Second)
	}()
	log.Println("[OK] Email stats flusher started (interval=1s)")

	// ==================== MX 自动验证轮询 ====================
	// 与域名接口一致：优先读后台设置 smtp_server_ip，未设置时回退到 SMTP_SERVER_IP 环境变量
	serverIPForMX := func() string {
		if ip, err := db.GetSetting(ctx, "smtp_server_ip"); err == nil && ip != "" {
			return ip
		}
		return cfg.SMTPServerIP
	}
	bgWG.Add(1)
	go func() {
		defer bgWG.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		log.Println("[OK] MX domain verifier started (pending check=30s, active re-check=6h)")
		reCheckTicker := time.NewTicker(6 * time.Hour)
		defer reCheckTicker.Stop()
		refreshActiveDomains := func() {
			serverIP := serverIPForMX()
			if serverIP == "" {
				log.Println("[mx-recheck] smtp_server_ip is empty, skipping active domain re-check")
				return
			}
			activeDomains, err := db.GetActiveDomains(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[mx-recheck] list active error: %v", err)
				}
				return
			}
			log.Printf("[mx-recheck] checking %d active domains", len(activeDomains))
			for _, d := range activeDomains {
				if ctx.Err() != nil {
					return
				}
				matched, details, mxStatus := store.CheckDomainMXDetails(ctx, d.Domain, serverIP)
				supportsSingle, supportsWildcard := store.DomainCapabilitiesFromMX(details)
				if err := db.UpdateDomainCapabilities(ctx, d.ID, supportsSingle, supportsWildcard); err != nil {
					if ctx.Err() == nil {
						log.Printf("[mx-recheck] update %s capabilities error: %v", d.Domain, err)
					}
					continue
				}
				if !matched {
					log.Printf("[mx-recheck] [WARN] %s MX no longer valid (%s), domain moved to pending", d.Domain, mxStatus)
				} else {
					log.Printf("[mx-recheck] %s capabilities refreshed, single=%v wildcard=%v", d.Domain, supportsSingle, supportsWildcard)
				}
			}
		}
		bgWG.Add(1)
		go func() {
			defer bgWG.Done()
			refreshActiveDomains()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 处理待验证域名
				serverIP := serverIPForMX()
				if serverIP == "" {
					continue
				}
				pendingDomains, err := db.ListPendingDomains(ctx)
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("[mx-verifier] list pending error: %v", err)
					}
					continue
				}
				if len(pendingDomains) == 0 {
					continue
				}
				for _, d := range pendingDomains {
					if ctx.Err() != nil {
						return
					}
					matched, details, mxStatus := store.CheckDomainMXDetails(ctx, d.Domain, serverIP)
					supportsSingle, supportsWildcard := store.DomainCapabilitiesFromMX(details)
					if err := db.UpdateDomainCapabilities(ctx, d.ID, supportsSingle, supportsWildcard); err != nil {
						if ctx.Err() == nil {
							log.Printf("[mx-verifier] update %s capabilities error: %v", d.Domain, err)
						}
						continue
					}
					if matched {
						log.Printf("[mx-verifier] [OK] %s MX verified, single=%v wildcard=%v", d.Domain, supportsSingle, supportsWildcard)
					} else {
						log.Printf("[mx-verifier] waiting: %s — %s", d.Domain, mxStatus)
					}
				}

			case <-reCheckTicker.C:
				// 每 6 小时重新检测所有已激活域名，MX 失效则自动移出活跃池
				refreshActiveDomains()
			}
		}
	}()

	// ==================== 写出管理员 API Key 文件 ====================
	bgWG.Add(1)
	go func() {
		defer bgWG.Done()
		// 等待 DB 就绪后再读取（延迟 1 秒）
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
		adminKey, err := db.GetAdminAPIKey(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("[adminkey] could not fetch admin key: %v", err)
			}
			return
		}
		keyFile := os.Getenv("ADMIN_KEY_FILE")
		if keyFile == "" {
			keyFile = "/data/admin.key"
		}
		if err := os.MkdirAll(filepath.Dir(keyFile), 0700); err == nil {
			content := "# TempMail Admin API Key\n# Auto-generated on startup — keep this secret!\n\nADMIN_API_KEY=" + adminKey + "\n"
			if err := os.WriteFile(keyFile, []byte(content), 0600); err != nil {
				log.Printf("[adminkey] write file error: %v", err)
			} else {
				log.Printf("[OK] Admin API Key written to %s", keyFile)
			}
		}
	}()

	// ==================== 启动服务 ====================
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("[OK] API server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
		_ = srv.Close()
	}
	if deliveryBatcher != nil {
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), 10*time.Second)
		if err := deliveryBatcher.CloseAndDrain(drainCtx); err != nil {
			log.Printf("Delivery batcher forced to stop: %v", err)
		}
		cancelDrain()
	}
	stopBackground()
	bgDone := make(chan struct{})
	go func() {
		bgWG.Wait()
		close(bgDone)
	}()
	select {
	case <-bgDone:
	case <-time.After(6 * time.Second):
		log.Printf("[shutdown] background workers timed out")
	}
	select {
	case <-statsFlusherDone:
	case <-time.After(6 * time.Second):
		log.Printf("[stats] final flush timed out")
	}
	log.Println("Server exited")
}

// internalDeliverMaxBodyBytes 限制 /internal/deliver* 请求体大小，避免超大载荷耗尽内存。
const internalDeliverMaxBodyBytes int64 = 12 << 20

// internalAuthMiddleware 保护 /internal/* 接口。
//   - internalKey 非空：要求请求头 X-Internal-Key 与之匹配（常量时间比较），否则 401。
//   - internalKey 为空：仅允许直连对端（RemoteAddr，不信任 X-Forwarded-For）为回环或私有网段地址，否则 403。
func internalAuthMiddleware(internalKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if internalKey != "" {
			provided := c.GetHeader("X-Internal-Key")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(internalKey)) != 1 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			c.Next()
			return
		}

		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			host = c.Request.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}

// maxBodySizeMiddleware 限制请求体大小：声明的 Content-Length 超限直接 413，
// 未声明长度（chunked）时由 http.MaxBytesReader 在读取超限时报错（绑定失败返回 400）。
func maxBodySizeMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}
