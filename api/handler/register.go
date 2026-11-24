package handler

import (
	"net/http"

	"tempmail/model"
	"tempmail/store"

	"github.com/gin-gonic/gin"
)

type RegisterHandler struct {
	store *store.Store
}

func NewRegisterHandler(s *store.Store) *RegisterHandler {
	return &RegisterHandler{store: s}
}

// POST /public/register → 公开注册（仅当 registration_open=true 时可用）
func (h *RegisterHandler) Register(c *gin.Context) {
	// 检查注册开关
	regOpen, err := h.store.GetSetting(c.Request.Context(), "registration_open")
	if err != nil || regOpen != "true" {
		c.JSON(http.StatusForbidden, gin.H{"error": "registration is currently closed"})
		return
	}

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
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":       account.ID,
		"username": account.Username,
		"api_key":  account.APIKey,
		"email":    account.Email,
		"message":  "registration successful — save your API key, it won't be shown again",
	})
}
