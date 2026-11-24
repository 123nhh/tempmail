package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
)

type MailboxHandler struct {
	store *store.Store
}

func NewMailboxHandler(s *store.Store) *MailboxHandler {
	return &MailboxHandler{store: s}
}

// POST /api/mailboxes - 创建临时邮箱
// 请求体字段均为可选：
//
//	address — 本地部分（@ 前），为空则随机生成
//	domain  — 指定域名（须是已激活域名），为空则优先使用默认域名，否则随机选取
//	mode    — single | multi，未指定时随机生成单域名或多级域名邮箱
//	subdomain — multi 模式下自定义子域名前缀，如 a.b.c
func (h *MailboxHandler) Create(c *gin.Context) {
	account := middleware.GetAccount(c)

	var req model.CreateMailboxReq
	_ = c.ShouldBindJSON(&req)

	address := strings.TrimSpace(req.Address)
	if address == "" {
		address = store.GenerateRandomAddress()
	}
	address = strings.ToLower(address)
	customSubdomain := strings.TrimSpace(req.Subdomain)

	ctx := c.Request.Context()

	// 读取 TTL 与每用户活跃邮箱上限设置
	ttlMinutes := 30
	if ttlStr, err := h.store.GetSetting(ctx, "mailbox_ttl_minutes"); err == nil {
		if n, err := strconv.Atoi(ttlStr); err == nil && n > 0 {
			ttlMinutes = n
		}
	}
	maxPerUser := 0
	if maxStr, err := h.store.GetSetting(ctx, "max_mailboxes_per_user"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(maxStr)); err == nil && n > 0 {
			maxPerUser = n
		}
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	randomMode := false
	if mode == "" {
		if req.DomainID > 0 || strings.TrimSpace(req.Domain) != "" {
			mode = "single"
		} else {
			randomMode = true
			if store.RandomBool() {
				mode = "single"
			} else {
				mode = "multi"
			}
		}
	}
	if mode != "single" && mode != "multi" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode, expected single or multi"})
		return
	}

	// 确定域名：指定 or 优先 default_domain or 随机。single 使用单域名能力，multi 使用通配子域能力。
	var domainRecord *model.Domain
	if req.DomainID > 0 {
		found, err := h.store.GetDomainByID(ctx, req.DomainID)
		if err != nil || !found.IsActive {
			c.JSON(http.StatusBadRequest, gin.H{"error": "domain not found or not active"})
			return
		}
		if req.Mode == "" {
			mode = "single"
			if !found.SupportsSingle && found.SupportsWildcard {
				mode = "multi"
			}
		}
		if mode == "single" && !found.SupportsSingle {
			c.JSON(http.StatusBadRequest, gin.H{"error": "single mode requires a domain with single-domain MX support"})
			return
		}
		if mode == "multi" && !found.SupportsWildcard {
			c.JSON(http.StatusBadRequest, gin.H{"error": "multi mode requires a domain with wildcard MX support"})
			return
		}
		domainRecord = found
	} else if d := strings.TrimSpace(strings.ToLower(req.Domain)); d != "" {
		found, err := h.store.GetDomainByName(ctx, d)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "domain not found or not active: " + d})
			return
		}
		if req.Mode == "" {
			mode = "single"
			if !found.SupportsSingle && found.SupportsWildcard {
				mode = "multi"
			}
		}
		if mode == "single" && !found.SupportsSingle {
			c.JSON(http.StatusBadRequest, gin.H{"error": "single mode requires a domain with single-domain MX support"})
			return
		}
		if mode == "multi" && !found.SupportsWildcard {
			c.JSON(http.StatusBadRequest, gin.H{"error": "multi mode requires a domain with wildcard MX support"})
			return
		}
		domainRecord = found
	} else {
		// 未指定域名时优先尝试管理员配置的 default_domain（若已激活且支持目标模式）
		if defName, derr := h.store.GetSetting(ctx, "default_domain"); derr == nil && strings.TrimSpace(defName) != "" {
			if cand, cerr := h.store.GetDomainByName(ctx, defName); cerr == nil && cand.IsActive {
				if (mode == "single" && cand.SupportsSingle) || (mode == "multi" && cand.SupportsWildcard) {
					domainRecord = cand
				} else if randomMode {
					if cand.SupportsSingle {
						mode = "single"
						domainRecord = cand
					} else if cand.SupportsWildcard {
						mode = "multi"
						domainRecord = cand
					}
				}
			}
		}
		if domainRecord == nil {
			capability := store.DomainCapabilitySingle
			if mode == "multi" {
				capability = store.DomainCapabilityWildcard
			}
			found, err := h.store.GetRandomActiveDomainByCapability(ctx, capability)
			if err != nil && randomMode {
				if mode == "multi" {
					found, err = h.store.GetRandomActiveDomainByCapability(ctx, store.DomainCapabilitySingle)
					if err == nil {
						mode = "single"
						capability = store.DomainCapabilitySingle
					}
				} else {
					found, err = h.store.GetRandomActiveDomainByCapability(ctx, store.DomainCapabilityWildcard)
					if err == nil {
						mode = "multi"
						capability = store.DomainCapabilityWildcard
					}
				}
			}
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("no active %s domains available", capability)})
				return
			}
			domainRecord = found
		}
	}

	domainName := domainRecord.BaseDomain
	if mode == "multi" {
		if customSubdomain != "" {
			customDomain, err := store.JoinCustomSubdomain(customSubdomain, domainRecord.BaseDomain)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			domainName = customDomain
		} else {
			domainName = store.GenerateMultiLevelDomain(domainRecord.BaseDomain)
		}
	}
	fullAddress := fmt.Sprintf("%s@%s", address, domainName)
	if len(domainName) > 253 || len(fullAddress) > 320 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mailbox address is too long"})
		return
	}

	mailbox, err := h.store.CreateMailboxWithLimit(ctx, account.ID, address, domainRecord.ID, fullAddress, ttlMinutes, maxPerUser)
	if err != nil {
		if errors.Is(err, store.ErrMailboxLimitReached) {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("已达到活跃邮箱数量上限（%d）", maxPerUser)})
			return
		}
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			c.JSON(http.StatusConflict, gin.H{"error": "address already taken, try again"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"mailbox": mailbox})
}

// GET /api/mailboxes/:id - 查询单个活跃邮箱详情
func (h *MailboxHandler) Get(c *gin.Context) {
	account := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	mailbox, err := h.store.GetMailbox(c.Request.Context(), id, account.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"mailbox": mailbox})
}

// GET /api/mailboxes - 列出当前账号的邮箱
func (h *MailboxHandler) List(c *gin.Context) {
	account := middleware.GetAccount(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	mailboxes, total, err := h.store.ListMailboxes(c.Request.Context(), account.ID, page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  mailboxes,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// DELETE /api/mailboxes/:id - 删除邮箱
func (h *MailboxHandler) Delete(c *gin.Context) {
	account := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mailbox id"})
		return
	}

	if err := h.store.DeleteMailbox(c.Request.Context(), id, account.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "mailbox not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "mailbox deleted"})
}
