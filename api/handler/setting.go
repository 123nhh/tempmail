package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"tempmail/store"

	"github.com/gin-gonic/gin"
)

const publicSettingsCacheTTL = 5 * time.Second

type settingStore interface {
	GetAllSettings(ctx context.Context) (map[string]string, error)
	SetSetting(ctx context.Context, key, value string) error
}

type SettingHandler struct {
	store settingStore

	mu       sync.Mutex
	cached   gin.H
	cachedAt time.Time
}

func NewSettingHandler(s *store.Store) *SettingHandler {
	return &SettingHandler{store: s}
}

func (h *SettingHandler) invalidateCache() {
	h.mu.Lock()
	h.cached = nil
	h.cachedAt = time.Time{}
	h.mu.Unlock()
}

// GET /public/settings → 返回前端需要的公开配置（单次批量查询 + 5 秒缓存）
func (h *SettingHandler) GetPublic(c *gin.Context) {
	h.mu.Lock()
	if h.cached != nil && time.Since(h.cachedAt) < publicSettingsCacheTTL {
		out := make(gin.H, len(h.cached))
		for k, v := range h.cached {
			out[k] = v
		}
		h.mu.Unlock()
		c.JSON(http.StatusOK, out)
		return
	}
	h.mu.Unlock()

	all, err := h.store.GetAllSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置暂时不可用"})
		return
	}

	pub := gin.H{
		"registration_open":     all["registration_open"] == "true",
		"key_login_enabled":     all["key_login_enabled"] != "false",
		"linuxdo_login_enabled": all["linuxdo_login_enabled"] == "true",
		"github_login_enabled":  all["github_login_enabled"] == "true",
		"site_title":            all["site_title"],
		"site_logo_url":         all["site_logo_url"],
		"smtp_server_ip":        all["smtp_server_ip"],
		"smtp_hostname":         all["smtp_hostname"],
	}

	h.mu.Lock()
	h.cached = pub
	h.cachedAt = time.Now()
	h.mu.Unlock()

	c.JSON(http.StatusOK, pub)
}

// GET /api/admin/settings → 读取所有设置（管理员）
func (h *SettingHandler) AdminGetAll(c *gin.Context) {
	settings, err := h.store.GetAllSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if secret := settings["linuxdo_client_secret"]; secret != "" {
		settings["linuxdo_client_secret_set"] = "true"
		settings["linuxdo_client_secret"] = ""
	} else {
		settings["linuxdo_client_secret_set"] = "false"
	}
	if secret := settings["github_client_secret"]; secret != "" {
		settings["github_client_secret_set"] = "true"
		settings["github_client_secret"] = ""
	} else {
		settings["github_client_secret_set"] = "false"
	}
	c.JSON(http.StatusOK, settings)
}

// PUT /api/admin/settings → 更新设置（管理员）
func (h *SettingHandler) AdminUpdate(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 白名单：已知配置项
	allowed := map[string]bool{
		"registration_open":            true,
		"max_mailboxes_per_user":       true,
		"smtp_server_ip":               true,
		"smtp_hostname":                true,
		"site_title":                   true,
		"site_logo_url":                true,
		"default_domain":               true,
		"mailbox_ttl_minutes":          true,
		"mailbox_extend_minutes":       true,
		"mailbox_max_lifetime_minutes": true,
		"key_login_enabled":            true,
		"linuxdo_login_enabled":        true,
		"linuxdo_client_id":            true,
		"linuxdo_client_secret":        true,
		"linuxdo_redirect_url":         true,
		"github_login_enabled":         true,
		"github_client_id":             true,
		"github_client_secret":         true,
		"github_redirect_url":          true,
	}

	// 先完整校验所有字段再落库，避免部分写入
	toWrite := make(map[string]string, len(req))
	for k, v := range req {
		if !allowed[k] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown setting key: " + k})
			return
		}
		if (k == "linuxdo_client_secret" || k == "github_client_secret") && v == "" {
			continue
		}
		if strings.HasSuffix(k, "_enabled") || k == "registration_open" {
			if v != "true" && v != "false" {
				c.JSON(http.StatusBadRequest, gin.H{"error": k + " must be true or false"})
				return
			}
		}
		if strings.HasSuffix(k, "_minutes") || k == "max_mailboxes_per_user" {
			if v != "" {
				n, err := strconv.Atoi(v)
				if err != nil || n < 0 || n > 525600 {
					c.JSON(http.StatusBadRequest, gin.H{"error": k + " must be an integer between 0 and 525600"})
					return
				}
				if k == "mailbox_ttl_minutes" && n == 0 {
					c.JSON(http.StatusBadRequest, gin.H{"error": "mailbox_ttl_minutes must be greater than 0"})
					return
				}
			}
		}
		toWrite[k] = v
	}

	for k, v := range toWrite {
		if err := h.store.SetSetting(c.Request.Context(), k, v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	h.invalidateCache()
	c.JSON(http.StatusOK, gin.H{"message": "settings updated"})
}
