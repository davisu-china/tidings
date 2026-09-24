// Package config 从环境变量加载全部配置。
// 密钥一律走环境变量，不写进代码，也不进版本库（见 .gitignore）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string
	HTTPAddr string

	// MediaBaseURL 是图片的对外基地址，如 https://host/img。
	// 留空则返回 /img/... 相对路径（前端与 Nginx 同源时用）。
	MediaBaseURL string
	// AdminToken 是运营后台的访问令牌，通过 X-Admin-Token 头传入。
	// 刻意不复用 Authorization：那个头装的是用户 JWT，两者混用会让
	// 「运营身份」和「用户身份」在代码里长得一样，混淆迟早出事。
	// 与用户 JWT 完全无关，运营身份不能当用户用，用户也进不了后台。
	AdminToken string
	// AdminEmails 是管理员邮箱白名单（逗号分隔），用于在客户端
	// 判断是否显示「运营后台」入口；后台接口本身仍靠 AdminToken 鉴权。
	AdminEmails string

	Postgres PostgresConfig
	Redis    RedisConfig
	MinIO    MinIOConfig
	JWT      JWTConfig
	Auth     AuthConfig
	Push     PushConfig
	Match    MatchConfig
	Intro    IntroConfig

	// Timezone 静默时段与各类日界的基准时区。
	Timezone string
}

type PostgresConfig struct {
	Host, User, Password, DBName string
	Port                         int
	SSLMode                      string
	MaxOpenConns, MaxIdleConns   int
}

func (p PostgresConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		p.Host, p.Port, p.User, p.Password, p.DBName, p.SSLMode)
}

// MigrateURL 是 golang-migrate 用的连接串，格式与 GORM 的 DSN 不同。
func (p PostgresConfig) MigrateURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.DBName, p.SSLMode)
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type MinIOConfig struct {
	// Endpoint 是服务端内部访问地址（compose 内网），用于拉取原图、
	// 写入缩略图等服务端操作。
	Endpoint string
	// PublicEndpoint 是浏览器能访问到的地址，专门用于生成预签名直传 URL。
	//
	// 必须分开配：预签名 V4 的签名把 Host 算了进去，签完再改写 URL
	// 会让签名失效。所以要用面向公网的端点单独签一次。
	// 留空时退回 Endpoint（本机开发两者相同的场景）。
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	Bucket         string
	UseSSL         bool
	PublicUseSSL   bool
	// PublicRead 决定桶是否开匿名只读。开启后浏览器可直接 GET 对象，
	// 图片走 Nginx 反代即可，不必下发会过期的预签名 GET URL。
	// 安全性依赖 object key 中的 UUID 不可猜 —— 策略里绝不能加 ListBucket。
	PublicRead bool
	// Region 显式指定，避免客户端为了确定区域去发 GetBucketLocation 请求。
	// 这对公网签名客户端是必须的：它只负责算签名，本身并不能连通
	// 那个地址（容器里的 localhost 是容器自己）。MinIO 默认 us-east-1。
	Region string
	// PresignExpiry 直传 URL 的有效期。
	PresignExpiry time.Duration
}

// ResolvedPublicEndpoint 返回实际用于签名的公网端点。
func (m MinIOConfig) ResolvedPublicEndpoint() string {
	if m.PublicEndpoint != "" {
		return m.PublicEndpoint
	}
	return m.Endpoint
}

type JWTConfig struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Issuer     string
}

// AuthConfig 是邮箱 + 密码登录的参数。
//
// 刻意没有验证码、没有找回密码、没有邮箱验证 —— 这三件事都需要
// 一条外发通道，而 MVP 只有 Web Push。代价写在风险清单里。
type AuthConfig struct {
	// BcryptCost 是 bcrypt 的代价因子。12 约 200–300ms，
	// 既是安全下限，也顺带成为撞库的天然刹车。
	BcryptCost int
	// MinPasswordLen 是最短密码长度。只限长度，不强制字符类别 ——
	// 复杂度规则会把用户逼向 Passw0rd! 这种更难记又没更强的密码。
	MinPasswordLen int
	// MaxAttempts 连续密码错误次数上限，超过锁 LockDuration。
	// 计数在 Redis，按邮箱 + IP 两个维度分别计。
	MaxAttempts  int
	LockDuration time.Duration
}

// PushConfig 是 Web Push（VAPID）配置。
type PushConfig struct {
	PublicKey  string
	PrivateKey string
	// Subject 是 VAPID 要求的联系地址，形如 mailto:ops@example.com。
	Subject string

	// QuietStart / QuietEnd 是静默时段（当地时间，24 小时制）。
	// 落在区间内的推送推迟到 QuietEnd 之后再发。
	QuietStart int
	QuietEnd   int
}

// Configured 判断 VAPID 密钥是否齐备。生产环境缺它是硬失败 ——
// 推送是这个产品的唯一触达通道，缺了等于整个核心循环不转。
func (p PushConfig) Configured() bool {
	return p.PublicKey != "" && p.PrivateKey != ""
}

// InQuietHours 判断给定时刻是否落在静默时段内。
// 区间跨零点（22 → 9）是常态，所以不能简单比较大小。
func (p PushConfig) InQuietHours(t time.Time) bool {
	h := t.Hour()
	if p.QuietStart == p.QuietEnd {
		return false
	}
	if p.QuietStart < p.QuietEnd {
		return h >= p.QuietStart && h < p.QuietEnd
	}
	return h >= p.QuietStart || h < p.QuietEnd
}

// MatchConfig 是匹配引擎的可调参数。
type MatchConfig struct {
	// Threshold 是双向匹配分阈值。调引擎时改这个。
	Threshold float64
	// ScanLimit 是候选集召回上限。
	ScanLimit int
}

// IntroConfig 是引荐生成任务的参数。
type IntroConfig struct {
	// GenInterval 是生成循环周期。
	GenInterval time.Duration
	// GenLease 是 job_runs 的租约时长，必须严格小于 GenInterval，
	// 否则上一轮的租约还没过期，下一轮就抢不到锁 —— 表现为任务静默停摆。
	GenLease time.Duration
	// BatchSize 是一次推送最多携带几条引荐。
	BatchSize int
	// PoolMin 低于此值时暂停自动引荐，向用户明示池子在攒人。
	PoolMin int
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:      env("APP_ENV", "development"),
		HTTPAddr: env("HTTP_ADDR", ":8080"),

		MediaBaseURL: strings.TrimRight(env("MEDIA_BASE_URL", ""), "/"),
		AdminToken:   env("ADMIN_TOKEN", ""),
		AdminEmails:  env("ADMIN_EMAILS", ""),

		Postgres: PostgresConfig{
			Host:         env("POSTGRES_HOST", "localhost"),
			Port:         envInt("POSTGRES_PORT", 5432),
			User:         env("POSTGRES_USER", "tidings"),
			Password:     env("POSTGRES_PASSWORD", ""),
			DBName:       env("POSTGRES_DB", "tidings"),
			SSLMode:      env("POSTGRES_SSLMODE", "disable"),
			MaxOpenConns: envInt("POSTGRES_MAX_OPEN_CONNS", 25),
			MaxIdleConns: envInt("POSTGRES_MAX_IDLE_CONNS", 5),
		},
		Redis: RedisConfig{
			Addr:     env("REDIS_ADDR", "localhost:6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		MinIO: MinIOConfig{
			Endpoint:       env("MINIO_ENDPOINT", "localhost:9000"),
			PublicEndpoint: env("MINIO_PUBLIC_ENDPOINT", ""),
			AccessKey:      env("MINIO_ACCESS_KEY", ""),
			SecretKey:      env("MINIO_SECRET_KEY", ""),
			Bucket:         env("MINIO_BUCKET", "tidings"),
			UseSSL:         envBool("MINIO_USE_SSL", false),
			PublicUseSSL:   envBool("MINIO_PUBLIC_USE_SSL", false),
			Region:         env("MINIO_REGION", "us-east-1"),
			PublicRead:     envBool("MINIO_PUBLIC_READ", true),
			PresignExpiry:  envDuration("MINIO_PRESIGN_EXPIRY", 5*time.Minute),
		},
		JWT: JWTConfig{
			Secret:     env("JWT_SECRET", ""),
			AccessTTL:  envDuration("JWT_ACCESS_TTL", 7*24*time.Hour),
			RefreshTTL: envDuration("JWT_REFRESH_TTL", 90*24*time.Hour),
			Issuer:     env("JWT_ISSUER", "tidings"),
		},
		Auth: AuthConfig{
			BcryptCost:     envInt("AUTH_BCRYPT_COST", 12),
			MinPasswordLen: envInt("AUTH_MIN_PASSWORD_LEN", 8),
			MaxAttempts:    envInt("AUTH_MAX_ATTEMPTS", 5),
			LockDuration:   envDuration("AUTH_LOCK_DURATION", 30*time.Minute),
		},
		Push: PushConfig{
			PublicKey:  env("VAPID_PUBLIC_KEY", ""),
			PrivateKey: env("VAPID_PRIVATE_KEY", ""),
			Subject:    env("VAPID_SUBJECT", ""),

			QuietStart: envInt("PUSH_QUIET_START", 22),
			QuietEnd:   envInt("PUSH_QUIET_END", 9),
		},
		Match: MatchConfig{
			Threshold: envFloat("MATCH_THRESHOLD", 0.55),
			ScanLimit: envInt("MATCH_SCAN_LIMIT", 200),
		},
		Intro: IntroConfig{
			GenInterval: envDuration("INTRO_GEN_INTERVAL", 5*time.Minute),
			GenLease:    envDuration("INTRO_GEN_LEASE", 4*time.Minute),
			BatchSize:   envInt("INTRO_BATCH_SIZE", 3),
			PoolMin:     envInt("INTRO_POOL_MIN", 100),
		},

		Timezone: env("TIMEZONE", "Asia/Shanghai"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Postgres.Password == "" {
		return fmt.Errorf("POSTGRES_PASSWORD 未设置")
	}
	if c.MinIO.AccessKey == "" || c.MinIO.SecretKey == "" {
		return fmt.Errorf("MINIO_ACCESS_KEY / MINIO_SECRET_KEY 未设置")
	}
	// JWT 密钥太短会让 HS256 签名可被暴力破解，这里直接拦掉
	if len(c.JWT.Secret) < 32 {
		return fmt.Errorf("JWT_SECRET 至少需要 32 字节，当前 %d", len(c.JWT.Secret))
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("TIMEZONE %q 无效: %w", c.Timezone, err)
	}
	if c.Intro.GenLease >= c.Intro.GenInterval {
		return fmt.Errorf("INTRO_GEN_LEASE(%s) 必须小于 INTRO_GEN_INTERVAL(%s)，否则租约永不释放，生成任务会在第一轮之后静默停摆",
			c.Intro.GenLease, c.Intro.GenInterval)
	}
	if c.Intro.BatchSize < 1 || c.Intro.BatchSize > 3 {
		return fmt.Errorf("INTRO_BATCH_SIZE 必须在 1–3 之间，当前 %d", c.Intro.BatchSize)
	}
	if c.Match.Threshold <= 0 || c.Match.Threshold > 1 {
		return fmt.Errorf("MATCH_THRESHOLD 必须在 (0, 1] 之间，当前 %v", c.Match.Threshold)
	}
	if c.Match.ScanLimit < 1 {
		return fmt.Errorf("MATCH_SCAN_LIMIT 必须大于 0，当前 %d", c.Match.ScanLimit)
	}
	for _, q := range []struct {
		name string
		v    int
	}{{"PUSH_QUIET_START", c.Push.QuietStart}, {"PUSH_QUIET_END", c.Push.QuietEnd}} {
		if q.v < 0 || q.v > 23 {
			return fmt.Errorf("%s 必须在 0–23 之间，当前 %d", q.name, q.v)
		}
	}

	if c.Auth.BcryptCost < 10 || c.Auth.BcryptCost > 15 {
		return fmt.Errorf("AUTH_BCRYPT_COST 必须在 10–15 之间，当前 %d", c.Auth.BcryptCost)
	}

	// 生产环境的硬约束。推送是这个产品唯一的触达通道 ——
	// 没有短信、没有邮件兜底，密钥缺失就是核心循环直接失效，
	// 不能等到第一封推送发不出去才发现。
	if c.Env == "production" && !c.Push.Configured() {
		return fmt.Errorf("生产环境必须配置 VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY")
	}
	return nil
}

// Location 返回静默时段与日切所用的时区。配置已在 validate 校验过，这里不会失败。
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// IsAdmin 判断邮箱是否在管理员白名单里。比较前统一转小写，
// 与 users 表那条 lower(email) 唯一索引保持同一套归一规则。
func (c *Config) IsAdmin(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	for _, e := range strings.Split(c.AdminEmails, ",") {
		if strings.ToLower(strings.TrimSpace(e)) == email {
			return true
		}
	}
	return false
}

// IsProduction 判断是否生产环境。
func (c *Config) IsProduction() bool { return c.Env == "production" }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
