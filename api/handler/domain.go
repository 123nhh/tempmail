package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tempmail/middleware"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const maxPendingDomains = 50

type DomainHandler struct {
	store       *store.Store
	cfgIP       string // SMTP_SERVER_IP env
	cfgHostname string // SMTP_HOSTNAME env
}

func NewDomainHandler(s *store.Store, smtpIP, smtpHostname string) *DomainHandler {
	return &DomainHandler{store: s, cfgIP: smtpIP, cfgHostname: smtpHostname}
}

// getServerIP 优先读 DB 设置，其次环境变量传入的值
func (h *DomainHandler) getServerIP(ctx context.Context) string {
	if ip, err := h.store.GetSetting(ctx, "smtp_server_ip"); err == nil && ip != "" {
		return ip
	}
	return h.cfgIP
}

// getServerHostname 返回 MX 记录应指向的邮件服务器 hostname
// 优先: DB 设置 smtp_hostname → 环境变量 → 空串（回退 mail.提交域名 方式）
func (h *DomainHandler) getServerHostname(ctx context.Context) string {
	if hn, err := h.store.GetSetting(ctx, "smtp_hostname"); err == nil && hn != "" {
		return hn
	}
	return h.cfgHostname
}

func mailHostForDomain(domain string) string {
	_, _, baseDomain := store.NormalizeDomain(domain)
	return "mail." + baseDomain
}

func dnsRecordsForDomain(domain, serverIP, hostname string) []gin.H {
	_, _, baseDomain := store.NormalizeDomain(domain)
	mxTarget := hostname
	if mxTarget == "" {
		mxTarget = mailHostForDomain(domain)
	}

	records := make([]gin.H, 0, 4)
	records = append(records,
		gin.H{"type": "MX", "host": baseDomain, "value": mxTarget, "priority": 10, "description": "单域名收信，如 user@" + baseDomain},
		gin.H{"type": "MX", "host": "*." + baseDomain, "value": mxTarget, "priority": 10, "description": "通配子域收信，如 user@a.b." + baseDomain},
		gin.H{"type": "TXT", "host": baseDomain, "value": fmt.Sprintf("v=spf1 ip4:%s ~all", serverIP), "description": "SPF 记录（可选）"},
	)
	if hostname == "" {
		records = append(records, gin.H{"type": "A", "host": mxTarget, "value": serverIP, "description": "邮件服务器 A 记录"})
	}
	return records
}

// POST /api/admin/domains - 添加域名到池（管理员）
func (h *DomainHandler) Add(c *gin.Context) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	domain, err := h.store.AddDomain(c.Request.Context(), req.Domain)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "domain already exists: " + err.Error()})
		return
	}

	// 获取服务器 IP 和 hostname（来自 DB 设置或环境变量）
	serverIP := h.getServerIP(c.Request.Context())
	hostname := h.getServerHostname(c.Request.Context())
	dnsRecords := dnsRecordsForDomain(req.Domain, serverIP, hostname)

	// 返回 DNS 配置指引
	c.JSON(http.StatusCreated, gin.H{
		"domain":      domain,
		"dns_records": dnsRecords,
		"instructions": fmt.Sprintf(
			"请在域名 %s 的 DNS 管理面板中添加以上记录。添加后约 5-30 分钟生效。",
			req.Domain),
	})
}

// GET /api/domains - 列出所有域名（共享域名池）
func (h *DomainHandler) List(c *gin.Context) {
	_ = middleware.GetAccount(c) // 确保已认证

	domains, err := h.store.ListDomains(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"domains": domains})
}

// DELETE /api/admin/domains/:id - 删除域名（管理员）
func (h *DomainHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	if err := h.store.DeleteDomain(c.Request.Context(), id); err != nil {
		var inUse *store.DomainInUseError
		if errors.As(err, &inUse) {
			c.JSON(http.StatusConflict, gin.H{
				"error":         fmt.Sprintf("该域名下仍有 %d 个邮箱，不能直接删除。请先删除相关邮箱，或停用该域名。", inUse.MailboxCount),
				"mailbox_count": inUse.MailboxCount,
			})
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "domain deleted"})
}

// PUT /api/admin/domains/:id/toggle - 启用/禁用域名（管理员）
func (h *DomainHandler) Toggle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.store.ToggleDomain(c.Request.Context(), id, req.Active); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		case errors.Is(err, store.ErrDomainNoCapability):
			c.JSON(http.StatusConflict, gin.H{"error": "域名尚未通过 MX 检测且无可用收信能力，无法启用"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "domain updated"})
}

// POST /api/admin/domains/mx-import - MX快捷接入（DNS检测并自动导入）
// body: {"domain":"example.com", "force":false}
func (h *DomainHandler) MXImport(c *gin.Context) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
		Force  bool   `json:"force"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))

	ctx := c.Request.Context()
	serverIP := h.getServerIP(ctx)
	hostname := h.getServerHostname(ctx)

	// DNS MX 检测
	matched, mxDetails, mxStatus := store.CheckDomainMXDetails(ctx, req.Domain, serverIP)
	supportsSingle, supportsWildcard := store.DomainCapabilitiesFromMX(mxDetails)

	if !matched && !req.Force {
		dnsHint := dnsRecordsForDomain(req.Domain, serverIP, hostname)
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":      "MX检测未通过，如确定要导入请加 force:true",
			"mx_status":  mxStatus,
			"mx_details": mxDetails,
			"server_ip":  serverIP,
			"domain":     req.Domain,
			"dns_hint":   dnsHint,
		})
		return
	}

	// force 导入且 MX 未通过：按域名形态推导能力，确保域名真正以激活状态进入域名池
	if !matched && req.Force {
		_, domainType, _ := store.NormalizeDomain(req.Domain)
		if domainType == store.DomainTypeWildcard {
			supportsWildcard = true
		} else {
			supportsSingle = true
		}
	}

	// 导入到域名池
	domain, err := h.store.AddDomainWithCapabilities(ctx, req.Domain, supportsSingle, supportsWildcard)
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "域名已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"domain":      domain,
		"mx_status":   mxStatus,
		"mx_details":  mxDetails,
		"mx_matched":  matched,
		"dns_records": dnsRecordsForDomain(req.Domain, serverIP, hostname),
		"message":     fmt.Sprintf("域名 %s 已导入域名池，Postfix 将在 60 秒内自动同步", req.Domain),
	})
}

// POST /api/admin/domains/mx-register - 管理员提交域名等待自动MX验证（允许重新激活已停用域名）
// body: {"domain":"example.com"}
func (h *DomainHandler) MXRegister(c *gin.Context) {
	h.submitDomain(c, true)
}

// POST /api/domains/submit — 任意已登录用户提交域名进行 MX 自动验证（禁止绕过管理员停用态，且受待验证队列上限限制）
func (h *DomainHandler) Submit(c *gin.Context) {
	h.submitDomain(c, false)
}

func (h *DomainHandler) submitDomain(c *gin.Context, allowReactivate bool) {
	var req struct {
		Domain string `json:"domain" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))

	ctx := c.Request.Context()
	if !allowReactivate {
		if existing, err := h.store.GetDomainByAnyName(ctx, req.Domain); err == nil && existing != nil && existing.Status == "disabled" {
			c.JSON(http.StatusForbidden, gin.H{"error": "该域名已被管理员停用，无法重新提交"})
			return
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	serverIP := h.getServerIP(ctx)
	hostname := h.getServerHostname(ctx)
	dnsRequired := dnsRecordsForDomain(req.Domain, serverIP, hostname)

	// 先尝试立即检测；通过则直接激活
	matched, mxDetails, mxStatus := store.CheckDomainMXDetails(ctx, req.Domain, serverIP)
	supportsSingle, supportsWildcard := store.DomainCapabilitiesFromMX(mxDetails)

	domain, err := h.store.SubmitDomain(ctx, req.Domain, matched, supportsSingle, supportsWildcard, allowReactivate, maxPendingDomains)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrDomainDisabled):
			c.JSON(http.StatusForbidden, gin.H{"error": "该域名已被管理员停用，无法重新提交"})
		case errors.Is(err, store.ErrPendingDomainsLimit):
			c.JSON(http.StatusTooManyRequests, gin.H{"error": fmt.Sprintf("待验证域名队列已满（上限 %d），请稍后再试", maxPendingDomains)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	if matched {
		c.JSON(http.StatusCreated, gin.H{
			"domain":       domain,
			"status":       domain.Status,
			"mx_status":    mxStatus,
			"mx_details":   mxDetails,
			"dns_required": dnsRequired,
			"message":      "MX验证通过，域名已立即加入域名池",
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"domain":       domain,
		"status":       domain.Status,
		"server_ip":    serverIP,
		"mx_status":    mxStatus,
		"mx_details":   mxDetails,
		"message":      fmt.Sprintf("域名 %s 已进入待验证队列，后台每30秒自动检测MX记录，通过后自动加入域名池", req.Domain),
		"dns_required": dnsRequired,
	})
}

// GET /api/domains/:id/status / GET /api/admin/domains/:id/status - 查询域名状态（用于前端轮询）
func (h *DomainHandler) GetStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid domain id"})
		return
	}

	ctx := c.Request.Context()
	domain, err := h.store.GetDomainByID(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	serverIP := h.getServerIP(ctx)
	_, mxDetails, mxStatus := store.CheckDomainMXDetails(ctx, domain.Domain, serverIP)

	c.JSON(http.StatusOK, gin.H{
		"id":                domain.ID,
		"domain":            domain.Domain,
		"supports_single":   domain.SupportsSingle,
		"supports_wildcard": domain.SupportsWildcard,
		"status":            domain.Status,
		"is_active":         domain.IsActive,
		"mx_checked_at":     domain.MxCheckedAt,
		"mx_status":         mxStatus,
		"mx_details":        mxDetails,
	})
}

// POST /api/admin/domains/refresh-mx - 手动刷新所有域名的单域名/通配子域 MX 能力
func (h *DomainHandler) RefreshMX(c *gin.Context) {
	ctx := c.Request.Context()
	domains, err := h.store.ListDomains(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	serverIP := h.getServerIP(ctx)
	if serverIP == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先在系统设置中配置服务器公网 IP (smtp_server_ip)"})
		return
	}

	results := make([]gin.H, 0, len(domains))
	for _, domain := range domains {
		if domain.Status == "disabled" {
			results = append(results, gin.H{
				"id":                domain.ID,
				"domain":            domain.Domain,
				"supports_single":   domain.SupportsSingle,
				"supports_wildcard": domain.SupportsWildcard,
				"is_active":         false,
				"mx_status":         "已由管理员停用",
				"mx_details":        []store.MXCheckDetail{},
			})
			continue
		}
		_, mxDetails, mxStatus := store.CheckDomainMXDetails(ctx, domain.Domain, serverIP)
		supportsSingle, supportsWildcard := store.DomainCapabilitiesFromMX(mxDetails)
		if err := h.store.UpdateDomainCapabilities(ctx, domain.ID, supportsSingle, supportsWildcard); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "domain": domain.Domain})
			return
		}
		results = append(results, gin.H{
			"id":                domain.ID,
			"domain":            domain.Domain,
			"supports_single":   supportsSingle,
			"supports_wildcard": supportsWildcard,
			"is_active":         supportsSingle || supportsWildcard,
			"mx_status":         mxStatus,
			"mx_details":        mxDetails,
		})
	}

	c.JSON(http.StatusOK, gin.H{"domains": results})
}
