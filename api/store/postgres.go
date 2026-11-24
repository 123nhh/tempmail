package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"tempmail/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// emailCounters 维护写入的累计数，避免每封邮件都 UPDATE 同一行。
// 由后台 flusher 周期性合并到数据库。
type emailCounters struct {
	mu      sync.Mutex
	total   int64
	mailbox map[uuid.UUID]int64
}

func (c *emailCounters) inc(mailboxID uuid.UUID) {
	c.mu.Lock()
	c.total++
	if c.mailbox == nil {
		c.mailbox = make(map[uuid.UUID]int64)
	}
	c.mailbox[mailboxID]++
	c.mu.Unlock()
}

func (c *emailCounters) incMany(mailboxIDs []uuid.UUID) {
	if len(mailboxIDs) == 0 {
		return
	}
	c.mu.Lock()
	c.total += int64(len(mailboxIDs))
	if c.mailbox == nil {
		c.mailbox = make(map[uuid.UUID]int64)
	}
	for _, mailboxID := range mailboxIDs {
		c.mailbox[mailboxID]++
	}
	c.mu.Unlock()
}

// snapshot 返回当前累计的内存计数（不清零，用于实时查询补偿）。
func (c *emailCounters) snapshot() (int64, map[uuid.UUID]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(map[uuid.UUID]int64, len(c.mailbox))
	for k, v := range c.mailbox {
		cp[k] = v
	}
	return c.total, cp
}

// drain 取出累计值并清零，供 flush 使用。
func (c *emailCounters) drain() (int64, map[uuid.UUID]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := c.total
	mb := c.mailbox
	c.total = 0
	c.mailbox = nil
	return total, mb
}

type Store struct {
	pool     *pgxpool.Pool
	counters emailCounters
}

type PoolOptions struct {
	MaxConns int32
	MinConns int32
}

const (
	DomainTypeExact    = "exact"
	DomainTypeWildcard = "wildcard"

	DomainCapabilitySingle   = "single"
	DomainCapabilityWildcard = "wildcard"
)

var (
	domainLabelPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	ErrCannotDeleteAdmin   = errors.New("cannot delete admin account")
	ErrDomainDisabled      = errors.New("domain is disabled by admin")
	ErrPendingDomainsLimit = errors.New("pending domain limit reached")
	ErrDomainNoCapability  = errors.New("domain has no verified MX capability")
	ErrMailboxLimitReached = errors.New("mailbox quota limit reached")
	ErrAccountDisabled     = errors.New("account is disabled")
)

const dnsLookupTimeout = 4 * time.Second

type DomainInUseError struct {
	MailboxCount int
}

func (e *DomainInUseError) Error() string {
	return fmt.Sprintf("domain is used by %d mailbox(es)", e.MailboxCount)
}

func NewWithOptions(ctx context.Context, dsn string, options PoolOptions) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	if options.MaxConns <= 0 {
		options.MaxConns = 128
	}
	if options.MinConns < 0 {
		options.MinConns = 0
	} else if options.MinConns == 0 {
		options.MinConns = 16
	}
	if options.MinConns > options.MaxConns {
		options.MinConns = options.MaxConns
	}
	cfg.MaxConns = options.MaxConns
	cfg.MinConns = options.MinConns
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	// PgBouncer transaction 模式不支持 named prepared statements。
	// pgx v5 默认使用 QueryExecModeCacheStatement（会发送 Parse/Bind/Execute），
	// 多个连接复用同一个后端连接时会触发 "prepared statement already in use"。
	// 改为 SimpleProtocol：直接发送明文 SQL，完全绕过服务端 prepared statement 机制。
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

// ==================== Account ====================

// accountColumns 与 scanAccount 的字段顺序保持一致（亦与 model.Account 字段顺序一致）。
const accountColumns = `id, username, api_key, COALESCE(linuxdo_id, ''), COALESCE(email, ''), is_admin, is_active, created_at, updated_at`

func scanAccount(row pgx.Row, a *model.Account) error {
	return row.Scan(&a.ID, &a.Username, &a.APIKey, &a.LinuxDOID, &a.Email, &a.IsAdmin, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
}

// EnsureSchema 幂等地补齐新版本新增的列，便于老库平滑升级。
func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS email VARCHAR(255) NOT NULL DEFAULT ''`)
	return err
}

func (s *Store) GetAccountByAPIKey(ctx context.Context, apiKey string) (*model.Account, error) {
	var a model.Account
	err := scanAccount(s.pool.QueryRow(ctx,
		`SELECT `+accountColumns+`
		 FROM accounts WHERE api_key = $1 AND is_active = TRUE`, apiKey,
	), &a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// getOrCreateOAuthAccount 按第三方 ID 查找账号；不存在则创建。
// idColumn 仅允许内部常量（linuxdo_id / github_id），不接受外部输入。
// 已存在但被停用的账号返回 ErrAccountDisabled，而不是尝试重复插入。
func (s *Store) getOrCreateOAuthAccount(ctx context.Context, idColumn, externalID, username, provider string) (*model.Account, error) {
	var a model.Account
	err := scanAccount(s.pool.QueryRow(ctx,
		`SELECT `+accountColumns+`
		 FROM accounts WHERE `+idColumn+` = $1`, externalID,
	), &a)
	if err == nil {
		if !a.IsActive {
			return nil, ErrAccountDisabled
		}
		return &a, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	apiKey := generateAPIKey()
	for i := 0; i < 5; i++ {
		name := username
		if i > 0 {
			name = fmt.Sprintf("%s_%d", username, i+1)
		}
		err = scanAccount(s.pool.QueryRow(ctx,
			`INSERT INTO accounts (username, api_key, `+idColumn+`) VALUES ($1, $2, $3)
			 RETURNING `+accountColumns,
			name, apiKey, externalID,
		), &a)
		if err == nil {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("failed to create %s account", provider)
}

func (s *Store) GetOrCreateLinuxDOAccount(ctx context.Context, linuxDOID, username string) (*model.Account, error) {
	return s.getOrCreateOAuthAccount(ctx, "linuxdo_id", linuxDOID, username, "Linux DO")
}

func (s *Store) GetOrCreateGitHubAccount(ctx context.Context, gitHubID, username string) (*model.Account, error) {
	return s.getOrCreateOAuthAccount(ctx, "github_id", gitHubID, username, "GitHub")
}

func (s *Store) CreateAccount(ctx context.Context, username string) (*model.Account, error) {
	return s.CreateAccountWithEmail(ctx, username, "")
}

// CreateAccountWithEmail 创建账号并记录可选联系邮箱（调用方负责校验/规范化 email）。
func (s *Store) CreateAccountWithEmail(ctx context.Context, username, email string) (*model.Account, error) {
	apiKey := generateAPIKey()
	var a model.Account
	err := scanAccount(s.pool.QueryRow(ctx,
		`INSERT INTO accounts (username, api_key, email) VALUES ($1, $2, $3)
		 RETURNING `+accountColumns,
		username, apiKey, email,
	), &a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) DeleteAccount(ctx context.Context, accountID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var isAdmin bool
	if err := tx.QueryRow(ctx, `SELECT is_admin FROM accounts WHERE id = $1 FOR UPDATE`, accountID).Scan(&isAdmin); err != nil {
		return err
	}
	if isAdmin {
		return ErrCannotDeleteAdmin
	}
	if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListAccounts 先取当前页账号，再仅对这一页账号做邮箱/邮件聚合，
// 避免对每个账号执行多次相关子查询。列顺序必须与 model.AccountWithStats 一致。
func (s *Store) ListAccounts(ctx context.Context, page, size int) ([]model.AccountWithStats, int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`WITH paged AS (
			SELECT id, username, api_key, linuxdo_id, email, is_admin, is_active, created_at, updated_at
			FROM accounts
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		),
		mb_stats AS (
			SELECT m.account_id,
				COUNT(*)::BIGINT AS mailbox_count,
				COUNT(*) FILTER (WHERE m.expires_at > NOW())::BIGINT AS active_mailbox_count,
				COALESCE(SUM(m.received_email_count), 0)::BIGINT AS received_email_count
			FROM mailboxes m
			WHERE m.account_id IN (SELECT id FROM paged)
			GROUP BY m.account_id
		),
		email_stats AS (
			SELECT m.account_id, COUNT(*)::BIGINT AS current_email_count
			FROM emails e
			JOIN mailboxes m ON m.id = e.mailbox_id
			WHERE m.account_id IN (SELECT id FROM paged)
			GROUP BY m.account_id
		)
		SELECT
			p.id, p.username, p.api_key, COALESCE(p.linuxdo_id, ''), COALESCE(p.email, ''), p.is_admin, p.is_active, p.created_at, p.updated_at,
			COALESCE(ms.mailbox_count, 0)::BIGINT AS mailbox_count,
			COALESCE(ms.active_mailbox_count, 0)::BIGINT AS active_mailbox_count,
			COALESCE(es.current_email_count, 0)::BIGINT AS current_email_count,
			COALESCE(ms.received_email_count, 0)::BIGINT AS received_email_count
		FROM paged p
		LEFT JOIN mb_stats ms ON ms.account_id = p.id
		LEFT JOIN email_stats es ON es.account_id = p.id
		ORDER BY p.created_at DESC`,
		size, (page-1)*size,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	accounts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.AccountWithStats])
	if err != nil {
		return nil, 0, err
	}
	return accounts, total, nil
}

// GetAdminAPIKey 获取第一个管理员账号的 API Key（用于写入 admin.key 文件）
func (s *Store) GetAdminAPIKey(ctx context.Context) (string, error) {
	var apiKey string
	err := s.pool.QueryRow(ctx,
		`SELECT api_key FROM accounts WHERE is_admin = TRUE ORDER BY created_at LIMIT 1`,
	).Scan(&apiKey)
	return apiKey, err
}

// ==================== Domain ====================

func (s *Store) AddDomain(ctx context.Context, domain string) (*model.Domain, error) {
	canonicalDomain, domainType, baseDomain := NormalizeDomain(domain)
	supportsSingle := domainType == DomainTypeExact
	supportsWildcard := domainType == DomainTypeWildcard
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`INSERT INTO domains (domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status)
		 VALUES ($1, $2, $3, $4, $5, TRUE, 'active')
		 RETURNING id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at`,
		canonicalDomain, domainType, baseDomain, supportsSingle, supportsWildcard,
	).Scan(&d.ID, &d.Domain, &d.DomainType, &d.BaseDomain, &d.SupportsSingle, &d.SupportsWildcard, &d.IsActive, &d.Status, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) AddDomainWithCapabilities(ctx context.Context, domain string, supportsSingle, supportsWildcard bool) (*model.Domain, error) {
	_, domainType, baseDomain := NormalizeDomain(domain)
	canonicalDomain := baseDomain
	if existing, err := s.GetDomainByAnyName(ctx, baseDomain); err == nil {
		canonicalDomain = existing.Domain
	}
	if supportsWildcard {
		domainType = DomainTypeWildcard
	}
	active := supportsSingle || supportsWildcard
	status := "pending"
	if active {
		status = "active"
	}
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`INSERT INTO domains (domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, mx_checked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		 ON CONFLICT (domain) DO UPDATE
		   SET domain_type = EXCLUDED.domain_type,
		       base_domain = EXCLUDED.base_domain,
		       supports_single = EXCLUDED.supports_single,
		       supports_wildcard = EXCLUDED.supports_wildcard,
		       is_active = EXCLUDED.is_active,
		       status = EXCLUDED.status,
		       mx_checked_at = NOW()
		 RETURNING id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at`,
		canonicalDomain, domainType, baseDomain, supportsSingle, supportsWildcard, active, status,
	).Scan(&d.ID, &d.Domain, &d.DomainType, &d.BaseDomain, &d.SupportsSingle, &d.SupportsWildcard, &d.IsActive, &d.Status, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// SubmitDomain 在单事务内原子保存用户或管理员提交的域名，防止并发覆写管理员停用态或绕过待验证队列上限。
func (s *Store) SubmitDomain(ctx context.Context, domain string, matched, supportsSingle, supportsWildcard, allowReactivate bool, maxPending int) (*model.Domain, error) {
	canonicalDomain, domainType, baseDomain := NormalizeDomain(domain)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing model.Domain
	err = tx.QueryRow(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains
		 WHERE domain = $1 OR base_domain = $2
		 ORDER BY CASE WHEN domain = $1 THEN 0 ELSE 1 END, id
		 LIMIT 1
		 FOR UPDATE`,
		canonicalDomain, baseDomain,
	).Scan(&existing.ID, &existing.Domain, &existing.DomainType, &existing.BaseDomain, &existing.SupportsSingle, &existing.SupportsWildcard, &existing.IsActive, &existing.Status, &existing.CreatedAt, &existing.MxCheckedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	if err == nil {
		if existing.Status == "disabled" && !allowReactivate {
			return nil, ErrDomainDisabled
		}
		if matched {
			newType := DomainTypeExact
			if supportsWildcard {
				newType = DomainTypeWildcard
			}
			var updated model.Domain
			if err := tx.QueryRow(ctx,
				`UPDATE domains
				 SET domain_type = $1,
				     base_domain = $2,
				     supports_single = $3,
				     supports_wildcard = $4,
				     is_active = TRUE,
				     status = 'active',
				     mx_checked_at = NOW()
				 WHERE id = $5
				 RETURNING id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at`,
				newType, baseDomain, supportsSingle, supportsWildcard, existing.ID,
			).Scan(&updated.ID, &updated.Domain, &updated.DomainType, &updated.BaseDomain, &updated.SupportsSingle, &updated.SupportsWildcard, &updated.IsActive, &updated.Status, &updated.CreatedAt, &updated.MxCheckedAt); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &updated, nil
		}
		if existing.Status == "disabled" && allowReactivate {
			var updated model.Domain
			if err := tx.QueryRow(ctx,
				`UPDATE domains
				 SET is_active = FALSE,
				     status = 'pending'
				 WHERE id = $1
				 RETURNING id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at`,
				existing.ID,
			).Scan(&updated.ID, &updated.Domain, &updated.DomainType, &updated.BaseDomain, &updated.SupportsSingle, &updated.SupportsWildcard, &updated.IsActive, &updated.Status, &updated.CreatedAt, &updated.MxCheckedAt); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &updated, nil
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &existing, nil
	}

	if matched {
		newType := domainType
		if supportsWildcard {
			newType = DomainTypeWildcard
		}
		var created model.Domain
		if err := tx.QueryRow(ctx,
			`INSERT INTO domains (domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, mx_checked_at)
			 VALUES ($1, $2, $3, $4, $5, TRUE, 'active', NOW())
			 RETURNING id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at`,
			baseDomain, newType, baseDomain, supportsSingle, supportsWildcard,
		).Scan(&created.ID, &created.Domain, &created.DomainType, &created.BaseDomain, &created.SupportsSingle, &created.SupportsWildcard, &created.IsActive, &created.Status, &created.CreatedAt, &created.MxCheckedAt); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &created, nil
	}

	if !allowReactivate && maxPending > 0 {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('domains_pending_submit'))`); err != nil {
			return nil, err
		}
		var pendingCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM domains WHERE status = 'pending'`).Scan(&pendingCount); err != nil {
			return nil, err
		}
		if pendingCount >= maxPending {
			return nil, ErrPendingDomainsLimit
		}
	}

	var created model.Domain
	if err := tx.QueryRow(ctx,
		`INSERT INTO domains (domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status)
		 VALUES ($1, $2, $3, FALSE, FALSE, FALSE, 'pending')
		 RETURNING id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at`,
		canonicalDomain, domainType, baseDomain,
	).Scan(&created.ID, &created.Domain, &created.DomainType, &created.BaseDomain, &created.SupportsSingle, &created.SupportsWildcard, &created.IsActive, &created.Status, &created.CreatedAt, &created.MxCheckedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *Store) ListDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

func (s *Store) GetActiveDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains WHERE is_active = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

func (s *Store) GetRandomActiveDomainByCapability(ctx context.Context, capability string) (*model.Domain, error) {
	condition := "supports_single = TRUE"
	if capability == DomainCapabilityWildcard {
		condition = "supports_wildcard = TRUE"
	}
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains WHERE is_active = TRUE AND %s
		 ORDER BY RANDOM() LIMIT 1`, condition),
	).Scan(&d.ID, &d.Domain, &d.DomainType, &d.BaseDomain, &d.SupportsSingle, &d.SupportsWildcard, &d.IsActive, &d.Status, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDomainByName 按域名字符串查找活跃域名，精确域名优先
func (s *Store) GetDomainByName(ctx context.Context, domain string) (*model.Domain, error) {
	canonicalDomain, _, baseDomain := NormalizeDomain(domain)
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains
		 WHERE is_active = TRUE AND (domain = $1 OR base_domain = $2)
		 ORDER BY CASE WHEN domain = $1 THEN 0 ELSE 1 END, id
		 LIMIT 1`,
		canonicalDomain, baseDomain,
	).Scan(&d.ID, &d.Domain, &d.DomainType, &d.BaseDomain, &d.SupportsSingle, &d.SupportsWildcard, &d.IsActive, &d.Status, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDomainByAnyName 不限激活状态查找域名，精确域名优先
func (s *Store) GetDomainByAnyName(ctx context.Context, domain string) (*model.Domain, error) {
	canonicalDomain, _, baseDomain := NormalizeDomain(domain)
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains
		 WHERE domain = $1 OR base_domain = $2
		 ORDER BY CASE WHEN domain = $1 THEN 0 ELSE 1 END, id
		 LIMIT 1`,
		canonicalDomain, baseDomain,
	).Scan(&d.ID, &d.Domain, &d.DomainType, &d.BaseDomain, &d.SupportsSingle, &d.SupportsWildcard, &d.IsActive, &d.Status, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetDomainByID(ctx context.Context, domainID int) (*model.Domain, error) {
	var d model.Domain
	err := s.pool.QueryRow(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains WHERE id = $1`,
		domainID,
	).Scan(&d.ID, &d.Domain, &d.DomainType, &d.BaseDomain, &d.SupportsSingle, &d.SupportsWildcard, &d.IsActive, &d.Status, &d.CreatedAt, &d.MxCheckedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListPendingDomains 返回所有待验证域名
func (s *Store) ListPendingDomains(ctx context.Context) ([]model.Domain, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, domain, domain_type, base_domain, supports_single, supports_wildcard, is_active, status, created_at, mx_checked_at
		 FROM domains WHERE status = 'pending'
		 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Domain])
}

// UpdateDomainCapabilities 刷新域名 MX 能力，但不覆盖管理员显式停用的域名。
func (s *Store) UpdateDomainCapabilities(ctx context.Context, domainID int, supportsSingle, supportsWildcard bool) error {
	active := supportsSingle || supportsWildcard
	status := "pending"
	if active {
		status = "active"
	}
	domainType := DomainTypeExact
	if supportsWildcard {
		domainType = DomainTypeWildcard
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE domains
		 SET supports_single = $1,
		     supports_wildcard = $2,
		     is_active = $3,
		     status = $4,
		     domain_type = $5,
		     mx_checked_at = NOW()
		 WHERE id = $6 AND status <> 'disabled'`,
		supportsSingle, supportsWildcard, active, status, domainType, domainID)
	return err
}

func (s *Store) DeleteDomain(ctx context.Context, domainID int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int
	if err := tx.QueryRow(ctx, `SELECT id FROM domains WHERE id = $1 FOR UPDATE`, domainID).Scan(&id); err != nil {
		return err
	}
	var mailboxCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM mailboxes WHERE domain_id = $1`, domainID).Scan(&mailboxCount); err != nil {
		return err
	}
	if mailboxCount > 0 {
		return &DomainInUseError{MailboxCount: mailboxCount}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM domains WHERE id = $1`, domainID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ToggleDomain(ctx context.Context, domainID int, active bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var supportsSingle, supportsWildcard bool
	if err := tx.QueryRow(ctx,
		`SELECT supports_single, supports_wildcard FROM domains WHERE id = $1 FOR UPDATE`,
		domainID,
	).Scan(&supportsSingle, &supportsWildcard); err != nil {
		return err
	}
	if active && !supportsSingle && !supportsWildcard {
		return ErrDomainNoCapability
	}

	status := "disabled"
	if active {
		status = "active"
	}
	// 停用时保留原有能力标记，以便管理员后续重新启用。
	if _, err := tx.Exec(ctx,
		`UPDATE domains SET is_active = $1, status = $2 WHERE id = $3`,
		active, status, domainID,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetStats 返回全局统计数据
func (s *Store) GetStats(ctx context.Context) (*model.Stats, error) {
	var st model.Stats
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM mailboxes)                         AS total_mailboxes,
		  (SELECT COUNT(*) FROM mailboxes WHERE expires_at > NOW()) AS active_mailboxes,
			  COALESCE((SELECT NULLIF(value, '')::BIGINT FROM app_settings WHERE key = 'total_emails_received'), (SELECT COUNT(*) FROM emails)) AS total_emails,
		  (SELECT COUNT(*) FROM domains WHERE is_active = TRUE)    AS active_domains,
		  (SELECT COUNT(*) FROM domains WHERE status = 'pending')  AS pending_domains,
		  (SELECT COUNT(*) FROM accounts WHERE is_active = TRUE)   AS total_accounts
	`).Scan(
		&st.TotalMailboxes, &st.ActiveMailboxes,
		&st.TotalEmails, &st.ActiveDomains,
		&st.PendingDomains, &st.TotalAccounts,
	)
	if err != nil {
		return nil, err
	}
	// 加上还没 flush 到数据库的内存计数，让前台看到接近实时值。
	pendingTotal, _ := s.counters.snapshot()
	st.TotalEmails += pendingTotal
	return &st, nil
}

// ==================== Mailbox ====================

func (s *Store) CreateMailboxWithLimit(ctx context.Context, accountID uuid.UUID, address string, domainID int, fullAddress string, ttlMinutes, maxPerUser int) (*model.Mailbox, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 30
	}
	expiresAt := time.Now().Add(time.Duration(ttlMinutes) * time.Minute)
	if maxPerUser <= 0 {
		var m model.Mailbox
		err := s.pool.QueryRow(ctx,
			`INSERT INTO mailboxes (account_id, address, domain_id, full_address, expires_at)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id, account_id, address, domain_id, full_address, created_at, expires_at`,
			accountID, address, domainID, fullAddress, expiresAt,
		).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.CreatedAt, &m.ExpiresAt)
		if err != nil {
			return nil, err
		}
		return &m, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 按账户加行锁，防止并发创建绕过每用户活跃邮箱上限
	var lockedID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id = $1 FOR UPDATE`, accountID).Scan(&lockedID); err != nil {
		return nil, err
	}
	var activeCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM mailboxes WHERE account_id = $1 AND expires_at > NOW()`,
		accountID,
	).Scan(&activeCount); err != nil {
		return nil, err
	}
	if activeCount >= maxPerUser {
		return nil, ErrMailboxLimitReached
	}

	var m model.Mailbox
	if err := tx.QueryRow(ctx,
		`INSERT INTO mailboxes (account_id, address, domain_id, full_address, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, account_id, address, domain_id, full_address, created_at, expires_at`,
		accountID, address, domainID, fullAddress, expiresAt,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.CreatedAt, &m.ExpiresAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListMailboxes(ctx context.Context, accountID uuid.UUID, page, size int) ([]model.Mailbox, int, error) {
	return s.ListMailboxesFiltered(ctx, accountID, page, size, "", "")
}

// escapeLikePattern 转义 LIKE/ILIKE 通配符（配合 ESCAPE '\' 使用）。
func escapeLikePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ListMailboxesFiltered 列出账号下活跃邮箱，支持按地址模糊搜索（query）与排序（sortBy）。
// sortBy: "expiry" 按到期时间升序；其他（"" / "new" / "created"）按创建时间倒序。
func (s *Store) ListMailboxesFiltered(ctx context.Context, accountID uuid.UUID, page, size int, query, sortBy string) ([]model.Mailbox, int, error) {
	where := `account_id = $1 AND expires_at > NOW()`
	args := []any{accountID}
	if query != "" {
		args = append(args, "%"+escapeLikePattern(query)+"%")
		where += ` AND full_address ILIKE $2 ESCAPE '\'`
	}
	orderBy := `created_at DESC`
	if sortBy == "expiry" {
		orderBy = `expires_at ASC, created_at DESC`
	}

	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM mailboxes WHERE `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	limitIdx := len(args) + 1
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`SELECT id, account_id, address, domain_id, full_address, created_at, expires_at
		 FROM mailboxes WHERE %s
		 ORDER BY %s LIMIT $%d OFFSET $%d`, where, orderBy, limitIdx, limitIdx+1),
		append(args, size, (page-1)*size)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	mailboxes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.Mailbox])
	if err != nil {
		return nil, 0, err
	}
	return mailboxes, total, nil
}

func (s *Store) GetMailbox(ctx context.Context, mailboxID uuid.UUID, accountID uuid.UUID) (*model.Mailbox, error) {
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`SELECT id, account_id, address, domain_id, full_address, created_at, expires_at
		 FROM mailboxes WHERE id = $1 AND account_id = $2 AND expires_at > NOW()`,
		mailboxID, accountID,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.CreatedAt, &m.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) DeleteMailbox(ctx context.Context, mailboxID uuid.UUID, accountID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM mailboxes WHERE id = $1 AND account_id = $2`, mailboxID, accountID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ErrExtendLimitReached 表示邮箱已达到最长存活时间，不能再续期。
var ErrExtendLimitReached = errors.New("mailbox lifetime limit reached")

// ExtendMailbox 把未过期邮箱的到期时间顺延 extendMinutes 分钟，
// 但不超过 created_at + maxLifetimeMinutes（<=0 表示不限）。已过期或不存在返回 pgx.ErrNoRows。
func (s *Store) ExtendMailbox(ctx context.Context, mailboxID, accountID uuid.UUID, extendMinutes, maxLifetimeMinutes int) (*model.Mailbox, error) {
	var m model.Mailbox
	err := s.pool.QueryRow(ctx,
		`UPDATE mailboxes
		 SET expires_at = CASE
		       WHEN $4::INT > 0 THEN LEAST(expires_at + make_interval(mins => $3::INT), created_at + make_interval(mins => $4::INT))
		       ELSE expires_at + make_interval(mins => $3::INT)
		     END
		 WHERE id = $1 AND account_id = $2 AND expires_at > NOW()
		   AND ($4::INT <= 0 OR expires_at < created_at + make_interval(mins => $4::INT))
		 RETURNING id, account_id, address, domain_id, full_address, created_at, expires_at`,
		mailboxID, accountID, extendMinutes, maxLifetimeMinutes,
	).Scan(&m.ID, &m.AccountID, &m.Address, &m.DomainID, &m.FullAddress, &m.CreatedAt, &m.ExpiresAt)
	if err == nil {
		return &m, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// 区分“已到上限”和“不存在/已过期”
	if _, getErr := s.GetMailbox(ctx, mailboxID, accountID); getErr == nil {
		return nil, ErrExtendLimitReached
	}
	return nil, pgx.ErrNoRows
}

// DeleteExpiredMailboxes 刪除已过期的邮箱（及其所有邮件）
func (s *Store) DeleteExpiredMailboxes(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM mailboxes WHERE expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// NormalizeDomain 将用户输入规整为存储格式。
// "*.example.com" 作为 wildcard 记录存储，base_domain 为 "example.com"。
func NormalizeDomain(domain string) (canonicalDomain, domainType, baseDomain string) {
	d := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(domain, ".")))
	if strings.HasPrefix(d, "*.") {
		base := strings.TrimPrefix(d, "*.")
		return "*." + base, DomainTypeWildcard, base
	}
	return d, DomainTypeExact, d
}

func DomainMXLookupName(domain string) string {
	_, domainType, baseDomain := NormalizeDomain(domain)
	if domainType == DomainTypeWildcard {
		return "mx-check.gmail.outlook.mail.com.net." + baseDomain
	}
	return baseDomain
}

type MXCheckDetail struct {
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Matched bool     `json:"matched"`
	MXHosts []string `json:"mx_hosts"`
	Status  string   `json:"status"`
}

func CheckDomainMXDetails(ctx context.Context, domain, serverIP string) (matched bool, details []MXCheckDetail, status string) {
	_, _, baseDomain := NormalizeDomain(domain)
	targets := []MXCheckDetail{
		{Kind: "single", Name: baseDomain},
		{Kind: "wildcard", Name: DomainMXLookupName("*." + baseDomain)},
	}

	anyMatched := false
	statuses := make([]string, 0, len(targets))
	for _, target := range targets {
		detail := checkMXName(ctx, target.Kind, target.Name, serverIP)
		if detail.Matched {
			anyMatched = true
		}
		statuses = append(statuses, fmt.Sprintf("%s: %s", detail.Name, detail.Status))
		details = append(details, detail)
	}
	return anyMatched, details, strings.Join(statuses, "；")
}

func DomainCapabilitiesFromMX(details []MXCheckDetail) (supportsSingle, supportsWildcard bool) {
	for _, detail := range details {
		switch detail.Kind {
		case "single", "exact", "base":
			if detail.Matched {
				supportsSingle = true
			}
		case "wildcard":
			if detail.Matched {
				supportsWildcard = true
			}
		}
	}
	return supportsSingle, supportsWildcard
}

func checkMXName(ctx context.Context, kind, lookupName, serverIP string) MXCheckDetail {
	detail := MXCheckDetail{Kind: kind, Name: lookupName}
	if ctx == nil {
		ctx = context.Background()
	}
	mxCtx, mxCancel := context.WithTimeout(ctx, dnsLookupTimeout)
	mxRecords, err := net.DefaultResolver.LookupMX(mxCtx, lookupName)
	mxCancel()
	if err != nil {
		detail.Status = fmt.Sprintf("DNS查询失败: %v", err)
		return detail
	}
	if len(mxRecords) == 0 {
		detail.Status = "未找到MX记录"
		return detail
	}
	for _, mx := range mxRecords {
		host := strings.TrimSuffix(mx.Host, ".")
		detail.MXHosts = append(detail.MXHosts, host)
		hostCtx, hostCancel := context.WithTimeout(ctx, dnsLookupTimeout)
		addrs, err := net.DefaultResolver.LookupHost(hostCtx, host)
		hostCancel()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if addr == serverIP {
				detail.Matched = true
				detail.Status = fmt.Sprintf("MX记录匹配：%s → %s", host, addr)
				return detail
			}
		}
	}
	detail.Status = fmt.Sprintf("MX记录(%s)未指向本服务器(%s)", strings.Join(detail.MXHosts, ","), serverIP)
	return detail
}

// ==================== Email ====================

// DeliveredEmail is an internal persistence result. Ordinal is one-based and
// matches the recipient's position in the request. Unknown recipients are
// returned with Delivered=false so the API can preserve its response order.
type DeliveredEmail struct {
	Ordinal   int64
	Recipient string
	EmailID   uuid.UUID
	MailboxID uuid.UUID
	Delivered bool
}

func (s *Store) InsertEmail(ctx context.Context, mailboxID uuid.UUID, sender, subject, bodyText, bodyHTML, raw string) (*model.Email, error) {
	e := model.Email{
		MailboxID:  mailboxID,
		Sender:     sender,
		Subject:    subject,
		BodyText:   bodyText,
		BodyHTML:   bodyHTML,
		RawMessage: raw,
		SizeBytes:  len(raw),
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO emails (mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, received_at`,
		mailboxID, sender, subject, bodyText, bodyHTML, raw, len(raw),
	).Scan(&e.ID, &e.ReceivedAt)
	if err != nil {
		return nil, err
	}
	// 热点累加放入内存计数器，由 RunStatsFlusher 异步 flush，避免高并发争抢同一行。
	s.counters.inc(mailboxID)
	return &e, nil
}

// DeliverEmails atomically resolves recipients and persists all matching
// deliveries in one PostgreSQL statement. It preserves input order, original
// recipient spelling, and duplicate recipients for API compatibility.
func (s *Store) DeliverEmails(ctx context.Context, recipients []string, sender, subject, bodyText, bodyHTML, raw string) ([]DeliveredEmail, error) {
	if len(recipients) == 0 {
		return []DeliveredEmail{}, nil
	}

	rows, err := s.pool.Query(ctx, `
		WITH input AS MATERIALIZED (
			SELECT recipient, ordinality::BIGINT AS ordinal
			FROM unnest($1::TEXT[]) WITH ORDINALITY AS u(recipient, ordinality)
		),
		matched AS MATERIALIZED (
			SELECT
				i.ordinal,
				i.recipient,
				m.id AS mailbox_id,
				gen_random_uuid() AS email_id
			FROM input AS i
			JOIN mailboxes AS m ON m.full_address = lower(i.recipient) AND m.expires_at > NOW()
		),
		inserted AS (
			INSERT INTO emails (id, mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes)
			SELECT email_id, mailbox_id, $2, $3, $4, $5, $6, $7
			FROM matched
			ORDER BY ordinal
			RETURNING id
		)
		SELECT
			i.ordinal,
			i.recipient,
			COALESCE(m.email_id, '00000000-0000-0000-0000-000000000000'::UUID),
			COALESCE(m.mailbox_id, '00000000-0000-0000-0000-000000000000'::UUID),
			(ins.id IS NOT NULL) AS delivered
		FROM input AS i
		LEFT JOIN matched AS m ON m.ordinal = i.ordinal
		LEFT JOIN inserted AS ins ON ins.id = m.email_id
		ORDER BY i.ordinal`,
		recipients, sender, subject, bodyText, bodyHTML, raw, len(raw),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]DeliveredEmail, 0, len(recipients))
	for rows.Next() {
		var result DeliveredEmail
		if err := rows.Scan(
			&result.Ordinal,
			&result.Recipient,
			&result.EmailID,
			&result.MailboxID,
			&result.Delivered,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(results) != len(recipients) {
		return nil, fmt.Errorf("delivery result count mismatch: got %d, want %d", len(results), len(recipients))
	}

	deliveredMailboxIDs := make([]uuid.UUID, 0, len(results))
	for _, result := range results {
		if result.Delivered {
			deliveredMailboxIDs = append(deliveredMailboxIDs, result.MailboxID)
		}
	}
	s.counters.incMany(deliveredMailboxIDs)
	return results, nil
}

// RunStatsFlusher 周期性把内存累计写回数据库。
// 在 main.go 启动后调用一次即可（goroutine 内运行）。
func (s *Store) RunStatsFlusher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.flushCounters(flushCtx)
			cancel()
			return
		case <-t.C:
			s.flushCounters(ctx)
		}
	}
}

func (s *Store) flushCounters(ctx context.Context) {
	total, mailboxes := s.counters.drain()
	if total == 0 && len(mailboxes) == 0 {
		return
	}

	mailboxIDs := make([]uuid.UUID, 0, len(mailboxes))
	for id := range mailboxes {
		mailboxIDs = append(mailboxIDs, id)
	}
	sort.Slice(mailboxIDs, func(i, j int) bool {
		return strings.Compare(mailboxIDs[i].String(), mailboxIDs[j].String()) < 0
	})
	type mailboxDelta struct {
		ID    string `json:"id"`
		Value int64  `json:"value"`
	}
	deltas := make([]mailboxDelta, len(mailboxIDs))
	for i, id := range mailboxIDs {
		deltas[i] = mailboxDelta{ID: id.String(), Value: mailboxes[id]}
	}
	deltasJSON, err := json.Marshal(deltas)

	var tx pgx.Tx
	if err == nil {
		tx, err = s.pool.Begin(ctx)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `
			INSERT INTO app_settings (key, value) VALUES ('total_emails_received', $1::TEXT)
			ON CONFLICT (key) DO UPDATE
			SET value = ((COALESCE(NULLIF(app_settings.value, ''), '0')::BIGINT + EXCLUDED.value::BIGINT)::TEXT), updated_at = NOW()
		`, total)
	}
	if err == nil && len(mailboxIDs) > 0 {
		_, err = tx.Exec(ctx, `
			UPDATE mailboxes AS m
			SET received_email_count = m.received_email_count + delta.value
			FROM jsonb_to_recordset($1::JSONB) AS delta(id UUID, value BIGINT)
			WHERE m.id = delta.id`,
			string(deltasJSON),
		)
	}
	if err == nil {
		err = tx.Commit(ctx)
	} else if tx != nil {
		_ = tx.Rollback(ctx)
	}
	if err == nil {
		return
	}

	// The transaction is all-or-nothing. Restore the entire drained snapshot
	// exactly once so a transient database failure cannot lose or double-count.
	s.counters.mu.Lock()
	s.counters.total += total
	if s.counters.mailbox == nil {
		s.counters.mailbox = make(map[uuid.UUID]int64, len(mailboxes))
	}
	for id, n := range mailboxes {
		s.counters.mailbox[id] += n
	}
	s.counters.mu.Unlock()
	if ctx.Err() == nil {
		// Avoid noisy shutdown logs while still surfacing live flush failures.
		log.Printf("[stats] flush failed: %v", err)
	}
}

// ListRecentEmailsByAccount 返回账号下所有活跃邮箱中最近收到的邮件摘要。
func (s *Store) ListRecentEmailsByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]model.RecentEmail, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT e.id, e.mailbox_id, m.full_address, e.sender, e.subject, e.size_bytes, e.received_at
		 FROM emails e
		 JOIN mailboxes m ON m.id = e.mailbox_id
		 WHERE m.account_id = $1 AND m.expires_at > NOW()
		 ORDER BY e.received_at DESC
		 LIMIT $2`,
		accountID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.RecentEmail])
}

func (s *Store) ListEmails(ctx context.Context, mailboxID uuid.UUID, page, size int) ([]model.EmailSummary, int, error) {
	var total int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM emails WHERE mailbox_id = $1`, mailboxID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, sender, subject, size_bytes, received_at
		 FROM emails WHERE mailbox_id = $1
		 ORDER BY received_at DESC LIMIT $2 OFFSET $3`,
		mailboxID, size, (page-1)*size,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	emails, err := pgx.CollectRows(rows, pgx.RowToStructByPos[model.EmailSummary])
	if err != nil {
		return nil, 0, err
	}
	return emails, total, nil
}

func (s *Store) GetEmail(ctx context.Context, emailID uuid.UUID, mailboxID uuid.UUID) (*model.Email, error) {
	var e model.Email
	err := s.pool.QueryRow(ctx,
		`SELECT id, mailbox_id, sender, subject, body_text, body_html, raw_message, size_bytes, received_at
		 FROM emails WHERE id = $1 AND mailbox_id = $2`,
		emailID, mailboxID,
	).Scan(&e.ID, &e.MailboxID, &e.Sender, &e.Subject, &e.BodyText, &e.BodyHTML, &e.RawMessage, &e.SizeBytes, &e.ReceivedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) DeleteEmail(ctx context.Context, emailID uuid.UUID, mailboxID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM emails WHERE id = $1 AND mailbox_id = $2`, emailID, mailboxID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ==================== Helpers ====================

func generateAPIKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "tm_" + hex.EncodeToString(b)
}

func GenerateRandomAddress() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	length := 10
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}

func RandomBool() bool {
	n, _ := rand.Int(rand.Reader, big.NewInt(2))
	return n.Int64() == 1
}

func GenerateMultiLevelDomain(baseDomain string) string {
	providers := []string{"gmail", "outlook", "hotmail", "yahoo", "icloud", "proton", "aol", "zoho"}
	mailWords := []string{"mail", "email", "inbox", "smtp", "mx", "webmail", "post"}
	tlds := []string{"com", "net", "org", "io", "co", "app", "dev"}
	pools := [][]string{providers, mailWords, tlds}

	pick := func(values []string) string {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(values))))
		return values[n.Int64()]
	}
	pickPool := func() []string {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(pools))))
		return pools[n.Int64()]
	}

	levelOffset, _ := rand.Int(rand.Reader, big.NewInt(5))
	labelCount := 10 + int(levelOffset.Int64())
	labels := make([]string, 0, labelCount)
	for len(labels) < labelCount {
		labels = append(labels, pick(pickPool()))
	}
	return strings.Join(labels, ".") + "." + strings.ToLower(strings.TrimSpace(baseDomain))
}

func JoinCustomSubdomain(subdomain, baseDomain string) (string, error) {
	labels := strings.Split(strings.ToLower(strings.Trim(strings.TrimSpace(subdomain), ".")), ".")
	if len(labels) == 0 {
		return "", fmt.Errorf("subdomain is required")
	}
	for _, label := range labels {
		if !domainLabelPattern.MatchString(label) {
			return "", fmt.Errorf("invalid subdomain label: %s", label)
		}
	}
	return strings.Join(labels, ".") + "." + strings.ToLower(strings.TrimSpace(baseDomain)), nil
}
