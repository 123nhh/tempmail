package handler

import (
	"errors"
	"net/mail"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

var errInvalidContactEmail = errors.New("invalid email address")

// normalizeContactEmail 校验并规范化可选联系邮箱。
// 空字符串（或纯空白）视为未填写，返回 ""。
func normalizeContactEmail(raw string) (string, error) {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	if cleaned == "" {
		return "", nil
	}
	if len(cleaned) > 255 || strings.ContainsAny(cleaned, "<>") || strings.IndexFunc(cleaned, unicode.IsSpace) >= 0 {
		return "", errInvalidContactEmail
	}
	addr, err := mail.ParseAddress(cleaned)
	if err != nil || addr.Address != cleaned || addr.Name != "" {
		return "", errInvalidContactEmail
	}
	at := strings.LastIndexByte(cleaned, '@')
	if at <= 0 || at == len(cleaned)-1 {
		return "", errInvalidContactEmail
	}
	local, domain := cleaned[:at], cleaned[at+1:]
	if len(local) > 64 || strings.ContainsAny(local, `"\`) {
		return "", errInvalidContactEmail
	}
	if !strings.Contains(domain, ".") || len(domain) > 253 {
		return "", errInvalidContactEmail
	}
	for _, label := range strings.Split(domain, ".") {
		if !isValidDomainLabel(label) {
			return "", errInvalidContactEmail
		}
	}
	return cleaned, nil
}

func isValidDomainLabel(label string) bool {
	if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// isUniqueViolation 判断是否为唯一约束冲突（PostgreSQL SQLSTATE 23505）。
// 对非 pgx 错误保留原有的字符串匹配兜底。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
