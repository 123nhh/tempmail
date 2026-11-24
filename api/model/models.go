package model

import (
	"time"

	"github.com/google/uuid"
)

// ==================== 数据模型 ====================

type Account struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	APIKey    string    `json:"api_key"`
	LinuxDOID string    `json:"linuxdo_id,omitempty"`
	Email     string    `json:"email,omitempty"`
	IsAdmin   bool      `json:"is_admin"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AccountWithStats struct {
	Account
	MailboxCount       int64 `json:"mailbox_count"`
	ActiveMailboxCount int64 `json:"active_mailbox_count"`
	CurrentEmailCount  int64 `json:"current_email_count"`
	ReceivedEmailCount int64 `json:"received_email_count"`
}

type Domain struct {
	ID               int        `json:"id"`
	Domain           string     `json:"domain"`
	DomainType       string     `json:"domain_type"` // exact | wildcard
	BaseDomain       string     `json:"base_domain"`
	SupportsSingle   bool       `json:"supports_single"`
	SupportsWildcard bool       `json:"supports_wildcard"`
	IsActive         bool       `json:"is_active"`
	Status           string     `json:"status"` // active | pending | disabled
	CreatedAt        time.Time  `json:"created_at"`
	MxCheckedAt      *time.Time `json:"mx_checked_at,omitempty"`
}

type Stats struct {
	TotalMailboxes  int64 `json:"total_mailboxes"`
	ActiveMailboxes int64 `json:"active_mailboxes"`
	TotalEmails     int64 `json:"total_emails"`
	ActiveDomains   int64 `json:"active_domains"`
	PendingDomains  int64 `json:"pending_domains"`
	TotalAccounts   int64 `json:"total_accounts"`
}

type Mailbox struct {
	ID          uuid.UUID `json:"id"`
	AccountID   uuid.UUID `json:"account_id"`
	Address     string    `json:"address"`
	DomainID    int       `json:"domain_id"`
	FullAddress string    `json:"full_address"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Email struct {
	ID         uuid.UUID `json:"id"`
	MailboxID  uuid.UUID `json:"mailbox_id"`
	Sender     string    `json:"sender"`
	Subject    string    `json:"subject"`
	BodyText   string    `json:"body_text"`
	BodyHTML   string    `json:"body_html"`
	RawMessage string    `json:"raw_message,omitempty"`
	SizeBytes  int       `json:"size_bytes"`
	ReceivedAt time.Time `json:"received_at"`
}

// ==================== 请求/响应 ====================

type CreateAccountReq struct {
	Username string `json:"username" binding:"required,min=2,max=64"`
	Email    string `json:"email,omitempty"` // 可选联系邮箱
}

type CreateMailboxReq struct {
	Address   string `json:"address,omitempty"` // 可选，为空则随机生成
	Domain    string `json:"domain,omitempty"`
	DomainID  int    `json:"domain_id,omitempty"`
	Mode      string `json:"mode,omitempty"`      // single | multi，未传时随机
	Subdomain string `json:"subdomain,omitempty"` // multi 模式下自定义多级子域名前缀
}

// RecentEmail 跨邮箱的最近邮件摘要（附带所属邮箱地址）
type RecentEmail struct {
	ID             uuid.UUID `json:"id"`
	MailboxID      uuid.UUID `json:"mailbox_id"`
	MailboxAddress string    `json:"mailbox_address"`
	Sender         string    `json:"sender"`
	Subject        string    `json:"subject"`
	SizeBytes      int       `json:"size_bytes"`
	ReceivedAt     time.Time `json:"received_at"`
}

type EmailSummary struct {
	ID         uuid.UUID `json:"id"`
	Sender     string    `json:"sender"`
	Subject    string    `json:"subject"`
	SizeBytes  int       `json:"size_bytes"`
	ReceivedAt time.Time `json:"received_at"`
}
