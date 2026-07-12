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
	SourceManual       = "manual"
	SourceSubscription = "subscription"
	legacySourceCustom = "custom"
)

func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite 单写

	s := &Storage{db: db}
	if err := s.initSchema(); err != nil {
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
			ai_reachability TEXT NOT NULL DEFAULT ''
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

	// 迁移：处理旧的 location 字段（如果存在）
	var hasOldLocation int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='location'`).Scan(&hasOldLocation)
	if err == nil && hasOldLocation > 0 {
		log.Println("[storage] migrating: renaming location to exit_location")
		// 如果有旧的 location 字段，先添加新字段再复制数据
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`)
		s.db.Exec(`UPDATE proxies SET exit_location = location WHERE location != ''`)
	}

	// 迁移：添加 exit_ip 字段
	var hasExitIP int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='exit_ip'`).Scan(&hasExitIP)
	if err == nil && hasExitIP == 0 {
		log.Println("[storage] migrating: adding exit_ip column")
		_, err = s.db.Exec(`ALTER TABLE proxies ADD COLUMN exit_ip TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("migrate exit_ip column: %w", err)
		}
	}

	// 迁移：添加 exit_location 字段
	var hasExitLocation int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='exit_location'`).Scan(&hasExitLocation)
	if err == nil && hasExitLocation == 0 {
		log.Println("[storage] migrating: adding exit_location column")
		_, err = s.db.Exec(`ALTER TABLE proxies ADD COLUMN exit_location TEXT NOT NULL DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("migrate exit_location column: %w", err)
		}
	}

	// 迁移：添加 latency 字段
	var hasLatency int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='latency'`).Scan(&hasLatency)
	if err == nil && hasLatency == 0 {
		log.Println("[storage] migrating: adding latency column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN latency INTEGER NOT NULL DEFAULT 0`)
	}

	// 迁移：添加质量等级字段
	var hasQuality int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='quality_grade'`).Scan(&hasQuality)
	if hasQuality == 0 {
		log.Println("[storage] migrating: adding quality_grade column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN quality_grade TEXT NOT NULL DEFAULT 'C'`)
	}

	// 迁移：添加使用统计字段
	var hasUseCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='use_count'`).Scan(&hasUseCount)
	if hasUseCount == 0 {
		log.Println("[storage] migrating: adding usage tracking columns")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN use_count INTEGER NOT NULL DEFAULT 0`)
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN success_count INTEGER NOT NULL DEFAULT 0`)
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN last_used DATETIME`)
	}

	// 迁移：添加状态字段
	var hasStatus int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='status'`).Scan(&hasStatus)
	if hasStatus == 0 {
		log.Println("[storage] migrating: adding status column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN status TEXT NOT NULL DEFAULT 'active'`)
	}

	// 迁移：添加 source 字段（区分订阅节点和手动节点）
	var hasSource int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='source'`).Scan(&hasSource)
	if hasSource == 0 {
		log.Println("[storage] migrating: adding source column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN source TEXT NOT NULL DEFAULT 'manual'`)
	}
	if _, err = s.db.Exec(`UPDATE proxies SET source = 'manual' WHERE source IN ('', 'free')`); err != nil {
		return fmt.Errorf("migrate proxy source values: %w", err)
	}
	if _, err = s.db.Exec(`UPDATE proxies SET source = ? WHERE source = ?`, SourceSubscription, legacySourceCustom); err != nil {
		return fmt.Errorf("migrate proxy source values: %w", err)
	}

	// 迁移：添加 subscription_id 字段
	var hasSubID int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('proxies') WHERE name='subscription_id'`).Scan(&hasSubID)
	if hasSubID == 0 {
		log.Println("[storage] migrating: adding subscription_id column")
		s.db.Exec(`ALTER TABLE proxies ADD COLUMN subscription_id INTEGER NOT NULL DEFAULT 0`)
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
	var hasContributed int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='contributed'`).Scan(&hasContributed)
	if hasContributed == 0 {
		s.db.Exec(`ALTER TABLE subscriptions ADD COLUMN contributed INTEGER NOT NULL DEFAULT 0`)
	}
	var hasLastSuccess int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='last_success'`).Scan(&hasLastSuccess)
	if hasLastSuccess == 0 {
		s.db.Exec(`ALTER TABLE subscriptions ADD COLUMN last_success DATETIME`)
	}
	// 迁移：订阅表添加 headers 字段（自定义请求头 JSON 字符串，向后兼容默认空）
	var hasHeaders int
	s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='headers'`).Scan(&hasHeaders)
	if hasHeaders == 0 {
		s.db.Exec(`ALTER TABLE subscriptions ADD COLUMN headers TEXT NOT NULL DEFAULT ''`)
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

// Close 关闭数据库
func (s *Storage) Close() error {
	return s.db.Close()
}

// GetDB 获取数据库实例（供其他模块使用）
func (s *Storage) GetDB() *sql.DB {
	return s.db
}
