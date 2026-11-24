package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeSettingStore struct {
	mu       sync.Mutex
	settings map[string]string
	getCalls int
}

func (f *fakeSettingStore) GetAllSettings(_ context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	out := make(map[string]string, len(f.settings))
	for k, v := range f.settings {
		out[k] = v
	}
	return out, nil
}

func (f *fakeSettingStore) SetSetting(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[key] = value
	return nil
}

func TestSettingHandlerPublicCacheAndInvalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fs := &fakeSettingStore{
		settings: map[string]string{
			"registration_open":     "true",
			"key_login_enabled":     "true",
			"linuxdo_login_enabled": "false",
			"github_login_enabled":  "true",
			"site_title":            "TempMail",
		},
	}
	h := &SettingHandler{store: fs}
	r := gin.New()
	r.GET("/public/settings", h.GetPublic)
	r.PUT("/api/admin/settings", h.AdminUpdate)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/public/settings", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d status=%d", i, w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body["site_title"] != "TempMail" || body["github_login_enabled"] != true {
			t.Fatalf("unexpected body: %#v", body)
		}
	}
	if fs.getCalls != 1 {
		t.Fatalf("expected 1 GetAllSettings call due to cache, got %d", fs.getCalls)
	}

	// Updating a setting must invalidate cache.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewBufferString(`{"site_title":"NewTitle"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/public/settings", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post-update status=%d", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["site_title"] != "NewTitle" {
		t.Fatalf("expected updated site_title, got %#v", body["site_title"])
	}
	if fs.getCalls != 2 {
		t.Fatalf("expected 2 GetAllSettings calls after invalidation, got %d", fs.getCalls)
	}
}

func TestSettingHandlerAdminUpdateValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		payload    string
		wantStatus int
		wantSaved  bool
	}{
		{
			name:       "reject removed rate_limit_enabled",
			payload:    `{"rate_limit_enabled":"true"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject unknown key before writing valid key",
			payload:    `{"site_title":"ShouldNotWrite","bogus":"1"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject non-bool enabled flag",
			payload:    `{"registration_open":"yes"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject zero mailbox_ttl_minutes",
			payload:    `{"mailbox_ttl_minutes":"0"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject negative max_mailboxes_per_user",
			payload:    `{"max_mailboxes_per_user":"-1"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject too large extend minutes",
			payload:    `{"mailbox_extend_minutes":"999999"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "accept valid numeric and empty optional settings",
			payload:    `{"mailbox_ttl_minutes":"45","mailbox_extend_minutes":"0","mailbox_max_lifetime_minutes":"1440","max_mailboxes_per_user":""}`,
			wantStatus: http.StatusOK,
			wantSaved:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &fakeSettingStore{settings: map[string]string{}}
			h := &SettingHandler{store: fs}
			r := gin.New()
			r.PUT("/api/admin/settings", h.AdminUpdate)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewBufferString(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status=%d want %d body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if !tt.wantSaved && len(fs.settings) != 0 {
				t.Fatalf("expected atomic reject with zero writes, got %#v", fs.settings)
			}
		})
	}
}
