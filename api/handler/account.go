package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"tempmail/middleware"
	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type AccountHandler struct {
	store *store.Store
}

func NewAccountHandler(s *store.Store) *AccountHandler {
	return &AccountHandler{store: s}
}

// POST /public/key-login - API Key 登录入口，可由后台开关关闭。
func (h *AccountHandler) KeyLogin(c *gin.Context) {
	keyLogin, _ := h.store.GetSetting(c.Request.Context(), "key_login_enabled")

	var req struct {
		APIKey string `json:"api_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	account, err := h.store.GetAccountByAPIKey(c.Request.Context(), strings.TrimSpace(req.APIKey))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api_key"})
		return
	}
	if keyLogin == "false" && !account.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "API Key login is disabled"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"api_key":    account.APIKey,
		"id":         account.ID,
		"username":   account.Username,
		"is_admin":   account.IsAdmin,
		"email":      account.Email,
		"created_at": account.CreatedAt,
	})
}

// POST /api/admin/accounts - 创建账号（管理员）
func (h *AccountHandler) Create(c *gin.Context) {
	var req model.CreateAccountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	email, err := normalizeContactEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email address"})
		return
	}

	account, err := h.store.CreateAccountWithEmail(c.Request.Context(), req.Username, email)
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists or db error: " + err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":       account.ID,
		"username": account.Username,
		"api_key":  account.APIKey,
		"email":    account.Email,
	})
}

// GET /api/admin/accounts - 列出所有账号（管理员）
func (h *AccountHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	accounts, total, err := h.store.ListAccounts(c.Request.Context(), page, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  accounts,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

// DELETE /api/admin/accounts/:id - 删除账号（管理员，禁止自删和删除管理员）
func (h *AccountHandler) Delete(c *gin.Context) {
	actor := middleware.GetAccount(c)
	id, err := parseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
		return
	}
	if actor != nil && actor.ID == id {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能删除当前登录账户"})
		return
	}

	if err := h.store.DeleteAccount(c.Request.Context(), id); err != nil {
		switch {
		case errors.Is(err, store.ErrCannotDeleteAdmin):
			c.JSON(http.StatusForbidden, gin.H{"error": "不能删除管理员账户"})
		case errors.Is(err, pgx.ErrNoRows):
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
}

// GET /api/me - 查看当前账号信息
func (h *AccountHandler) Me(c *gin.Context) {
	account := middleware.GetAccount(c)
	c.JSON(http.StatusOK, gin.H{
		"id":         account.ID,
		"username":   account.Username,
		"is_admin":   account.IsAdmin,
		"email":      account.Email,
		"created_at": account.CreatedAt,
	})
}
