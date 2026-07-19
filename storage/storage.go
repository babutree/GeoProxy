package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Proxy struct {
	ID             int64     `json:"id"`
	Address        string    `json:"address"`
	Protocol       string    `json:"protocol"`
	Region         string    `json:"region"`
	RegionSource   string    `json:"region_source"`
	Note           string    `json:"note"`
	ExitIP         string    `json:"exit_ip"`
	ExitLocation   string    `json:"exit_location"`
	Latency        int       `json:"latency"`
	QualityGrade   string    `json:"quality_grade"`
	UseCount       int       `json:"use_count"`
	SuccessCount   int       `json:"success_count"`
	FailCount      int       `json:"fail_count"`
	LastUsed       time.Time `json:"last_used"`
	LastCheck      time.Time `json:"last_check"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"`
	UserPaused     bool      `json:"user_paused"`
	Source         string    `json:"source"`          // "subscription" 订阅节点或 "manual" 手动节点
	SubscriptionID int64     `json:"subscription_id"` // 所属订阅ID（0=手动节点）
	// IP 风险信号：两源分开展示、不聚合。
	IPAPIIsScore   float64 `json:"ipapiis_score"`    // ipapi.is abuser_score（0-1，越高越危险）；-1 表示未探测/查询失败
	IPAPIFlags     string  `json:"ipapi_flags"`      // ip-api 命中标记逗号拼接（proxy/hosting/mobile）；空=本次探测无命中或未知
	IPAPIFlagsSeen bool    `json:"ipapi_flags_seen"` // ip-api proxy/hosting/mobile 是否已完成探测；false 时前端显示未知
	Starred        bool    `json:"starred"`
	CFBlocked      int     `json:"cf_blocked"` // -1未探测 0未拦截 1被拦截
	// DualProtocol 标记该节点的本地端口是否为 sing-box mixed 入站（单端口同时服务 SOCKS5+HTTP）。
	// 拨号/验证/存储 protocol 仍为 socks5（mixed 端口对 socks5 客户端即标准 socks5）；
	// 该字段仅供前端可靠渲染双协议标签，避免靠地址长相猜测而误判本机 direct socks5 节点。
	DualProtocol bool `json:"dual_protocol"`
	// AIReachability 存 4 个 AI 服务可达性的 JSON 对象字符串，如
	// {"openai":0,"claude":1,"grok":-1,"gemini":0}。值语义：-1未探测 0可达 1不可达。
	// 空字符串表示整体未探测。
	AIReachability string `json:"ai_reachability"`
	// Username/Password 是上游 http/socks 代理的认证凭据（若节点需要认证）。
	// 拨号/验证时注入到出站握手；空串表示无需认证。凭据绝不写入日志或错误串。
	Username string `json:"username"`
	Password string `json:"password"`
	// NodeKey 稳定节点身份（custom.ParsedNode.NodeKey：type/server/port/raw 哈希）。
	// 隧道节点 address 可能是 127.0.0.1:临时端口，刷新后会变；NodeKey 不变，供 -node-key- 锁定与 upsert 保 id。
	// 直连节点可为空或与 address 对齐的派生键。空串表示旧数据/未写入。
	NodeKey string `json:"node_key"`
}

// Subscription 订阅信息
type Subscription struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	FilePath    string    `json:"file_path"`
	Format      string    `json:"format"` // clash / plain / base64 / auto
	RefreshMin  int       `json:"refresh_min"`
	LastFetch   time.Time `json:"last_fetch"`
	LastSuccess time.Time `json:"last_success"` // 最后一次有可用节点的时间
	Status      string    `json:"status"`       // active / paused
	ProxyCount  int       `json:"proxy_count"`
	CreatedAt   time.Time `json:"created_at"`
	Contributed bool      `json:"contributed"` // 是否为访客贡献
	Headers     string    `json:"headers"`     // 自定义请求头 JSON 对象字符串，如 {"User-Agent":"clash.meta"}；空=使用默认 UA
}

type Storage struct {
	db *sql.DB
}

const (
	SourceManual                   = "manual"
	SourceSubscription             = "subscription"
	legacySourceCustom             = "custom"
	selectableSubscriptionScopeSQL = `(proxies.source != 'subscription' OR EXISTS (
		SELECT 1 FROM subscriptions
		WHERE subscriptions.id = proxies.subscription_id
		  AND subscriptions.status != 'paused'
	))`
)

func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite 单写

	s := &Storage{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Storage) initSchema() error {
	// 创建代理表
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS proxies (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			address        TEXT NOT NULL,
			protocol       TEXT NOT NULL,
			region         TEXT NOT NULL DEFAULT '',
			region_source  TEXT NOT NULL DEFAULT '',
			note           TEXT NOT NULL DEFAULT '',
			exit_ip        TEXT NOT NULL DEFAULT '',
			exit_location  TEXT NOT NULL DEFAULT '',
			latency        INTEGER NOT NULL DEFAULT 0,
			quality_grade  TEXT NOT NULL DEFAULT 'C',
			use_count      INTEGER NOT NULL DEFAULT 0,
			success_count  INTEGER NOT NULL DEFAULT 0,
			fail_count     INTEGER NOT NULL DEFAULT 0,
			last_used      DATETIME,
			last_check     DATETIME,
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status         TEXT NOT NULL DEFAULT 'active',
			user_paused    INTEGER NOT NULL DEFAULT 0,
			source         TEXT NOT NULL DEFAULT 'manual',
			subscription_id INTEGER NOT NULL DEFAULT 0,
			ipapiis_score  REAL NOT NULL DEFAULT -1,
			ipapi_flags    TEXT NOT NULL DEFAULT '',
			ipapi_flags_seen INTEGER NOT NULL DEFAULT 0,
			starred        INTEGER NOT NULL DEFAULT 0,
			cf_blocked     INTEGER NOT NULL DEFAULT -1,
			dual_protocol  INTEGER NOT NULL DEFAULT 0,
			ai_reachability TEXT NOT NULL DEFAULT '',
			proxy_username TEXT NOT NULL DEFAULT '',
			proxy_password TEXT NOT NULL DEFAULT '',
			node_key       TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		return err
	}

	// 旧公共源断路器表不再服务 geo gateway，显式删除以避免继续依赖公共源状态。
	_, err = s.db.Exec(`DROP TABLE IF EXISTS source_status`)
	if err != nil {
		return err
	}

	if err := s.migrateLocationColumn(); err != nil {
		return err
	}

	// 迁移：添加 exit_ip 字段
	if err := s.addProxyColumnIfMissing("exit_ip", `ALTER TABLE proxies ADD COLUMN exit_ip TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}

	// 迁移：添加 exit_location 字段
	if err := s.addProxyColumnIfMissing("exit_location", `ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}

	// 迁移：添加 latency 字段
	if err := s.addProxyColumnIfMissing("latency", `ALTER TABLE proxies ADD COLUMN latency INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}

	// 迁移：添加质量等级字段
	if err := s.addProxyColumnIfMissing("quality_grade", `ALTER TABLE proxies ADD COLUMN quality_grade TEXT NOT NULL DEFAULT 'C'`); err != nil {
		return err
	}

	// 迁移：添加使用统计字段
	if err := s.addProxyColumnIfMissing("use_count", `ALTER TABLE proxies ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.addProxyColumnIfMissing("success_count", `ALTER TABLE proxies ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.addProxyColumnIfMissing("last_used", `ALTER TABLE proxies ADD COLUMN last_used DATETIME`); err != nil {
		return err
	}

	// 迁移：添加状态字段
	if err := s.addProxyColumnIfMissing("status", `ALTER TABLE proxies ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`); err != nil {
		return err
	}

	// 迁移：添加 source 字段（区分订阅节点和手动节点）
	if err := s.addProxyColumnIfMissing("source", `ALTER TABLE proxies ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`UPDATE proxies SET source = 'manual' WHERE source IN ('', 'free')`); err != nil {
		return fmt.Errorf("migrate proxy source values: %w", err)
	}
	if _, err = s.db.Exec(`UPDATE proxies SET source = ? WHERE source = ?`, SourceSubscription, legacySourceCustom); err != nil {
		return fmt.Errorf("migrate proxy source values: %w", err)
	}

	// 迁移：添加 subscription_id 字段
	if err := s.addProxyColumnIfMissing("subscription_id", `ALTER TABLE proxies ADD COLUMN subscription_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.migrateRequiredProxyColumns(); err != nil {
		return err
	}
	if err := s.migrateProxyGeoColumns(); err != nil {
		return err
	}
	if err := s.rebuildProxiesWithoutAddressUnique(); err != nil {
		return err
	}

	// 创建订阅表
	_, err = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS subscriptions (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			name          TEXT NOT NULL DEFAULT '',
			url           TEXT NOT NULL DEFAULT '',
			file_path     TEXT NOT NULL DEFAULT '',
			format        TEXT NOT NULL DEFAULT 'clash',
			refresh_min   INTEGER NOT NULL DEFAULT 60,
			last_fetch    DATETIME,
			status        TEXT NOT NULL DEFAULT 'active',
			proxy_count   INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return err
	}

	// 迁移：订阅表添加 contributed 和 last_success 字段
	if err := s.addSubscriptionColumnIfMissing("contributed", `ALTER TABLE subscriptions ADD COLUMN contributed INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.addSubscriptionColumnIfMissing("last_success", `ALTER TABLE subscriptions ADD COLUMN last_success DATETIME`); err != nil {
		return err
	}
	// 迁移：订阅表添加 headers 字段（自定义请求头 JSON 字符串，向后兼容默认空）
	if err := s.addSubscriptionColumnIfMissing("headers", `ALTER TABLE subscriptions ADD COLUMN headers TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := s.migrateSubscriptionIdentity(); err != nil {
		return err
	}
	if err := s.migrateProxyIdentity(); err != nil {
		return err
	}

	// 创建索引
	if _, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_protocol_latency ON proxies(protocol, latency)`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_quality_grade ON proxies(quality_grade, latency)`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_status ON proxies(status)`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_region_status_latency ON proxies(region, status, latency)`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_source ON proxies(source, status)`); err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_proxy_identity ON proxies(address, source, subscription_id)`); err != nil {
		return fmt.Errorf("create proxy identity index: %w", err)
	}
	if _, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_url_unique ON subscriptions(url) WHERE url != ''`); err != nil {
		return fmt.Errorf("create subscription url unique index: %w", err)
	}
	if _, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_file_path_unique ON subscriptions(file_path) WHERE file_path != ''`); err != nil {
		return fmt.Errorf("create subscription file_path unique index: %w", err)
	}

	return nil
}

func (s *Storage) migrateLocationColumn() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin location migration: %w", err)
	}
	defer tx.Rollback()

	var hasOldLocation int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name = 'location'`).Scan(&hasOldLocation); err != nil {
		return fmt.Errorf("check proxies.location column: %w", err)
	}
	if hasOldLocation == 0 {
		return tx.Commit()
	}

	log.Println("[storage] 正在迁移：将 location 重命名为 exit_location")
	if err := addProxyColumnIfMissing(tx, "exit_location", `ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE proxies SET exit_location = location WHERE location != ''`); err != nil {
		return fmt.Errorf("migrate location values: %w", err)
	}
	return tx.Commit()
}

// Close 关闭数据库
func (s *Storage) Close() error {
	return s.db.Close()
}

// GetDB 获取数据库实例（供其他模块使用）
func (s *Storage) GetDB() *sql.DB {
	return s.db
}
