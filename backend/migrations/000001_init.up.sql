-- Tidings · 初始 schema
--
-- 关于 NULL 的约定：
-- profiles 里那 6 个「必填」字段在数据库层是可空的。建档是分步向导，
-- 每一步都要能落库，用户可能中途退出，写 NOT NULL 会让每一步都失败。
-- 完整性由 service 层在把 users.status 从 onboarding 翻成 active 之前校验，
-- 而所有候选集查询都带 status = 'active'，所以半成品档案永远不会被看到。
--
-- 关于状态列：一律用 TEXT + CHECK，不用 PG 的 ENUM 类型。
-- ENUM 加值要 ALTER TYPE，回滚麻烦；TEXT + CHECK 改一行约束即可。

-- ---------------------------------------------------------------- users
CREATE TABLE users (
    id             BIGSERIAL PRIMARY KEY,
    email          TEXT        NOT NULL,
    -- bcrypt 哈希。留 DEFAULT '' 是为了让「有账号但没设密码」
    -- （将来可能的第三方登录）在库里可表达；bcrypt 比对空串必然失败，
    -- 所以这个默认值不会变成一条能登录的后门。
    password_hash  TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT 'onboarding',
    token_version  INTEGER     NOT NULL DEFAULT 0,   -- 全局 JWT 开关，封禁时递增
    last_active_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_status_chk CHECK (
        status IN ('onboarding', 'active', 'under_review', 'banned', 'deactivated')
    ),
    CONSTRAINT users_email_len_chk CHECK (char_length(email) BETWEEN 3 AND 254)
);

-- 大小写不敏感唯一。用函数索引而不是 CITEXT 扩展：不引扩展，
-- 也不依赖 service 层每次都记得转小写 —— 漏一次就是两个账号。
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- 候选池只含 active，部分索引才小
CREATE INDEX idx_users_active ON users (id) WHERE status = 'active';

-- ------------------------------------------------------------- profiles
CREATE TABLE profiles (
    user_id          BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,

    -- 必填 6 项
    nickname         TEXT,
    gender           CHAR(1),          -- M / F，设定后不可改；匹配一律异性，故无 seeking 字段
    birth_ym         INTEGER,          -- 如 199505
    city_code        INTEGER,          -- 国标 6 位
    height_cm        SMALLINT,
    education_level  SMALLINT,         -- 1 大专及以下 / 2 本科 / 3 硕士 / 4 博士

    -- 选填 12 项，构成深度档案
    hometown_code    INTEGER,
    weight_kg        SMALLINT,
    school_name      TEXT     NOT NULL DEFAULT '',
    school_tier      SMALLINT,         -- 1..5，仅服务端按院校库归一，不接受前端传值
    occupation       TEXT     NOT NULL DEFAULT '',
    company          TEXT     NOT NULL DEFAULT '',
    income_band      SMALLINT,         -- 1..6，对外只出区间不出具体数字
    chronotype       SMALLINT,         -- 1 早睡早起 / 2 夜猫子 / 3 不规律
    smoking          SMALLINT,         -- 0 不 / 1 偶尔 / 2 经常
    drinking         SMALLINT,
    hobbies          TEXT     NOT NULL DEFAULT '',  -- 、连接，≤6 个
    intro            TEXT     NOT NULL DEFAULT '',
    expectation      TEXT     NOT NULL DEFAULT '',

    avatar_key       TEXT     NOT NULL DEFAULT '',
    completeness     SMALLINT NOT NULL DEFAULT 0,   -- 0..100，不参与打分，只做门槛

    -- 内容审核：文本签名比对命中才标记待审核；被拒时从快照恢复
    profile_review_state TEXT NOT NULL DEFAULT 'approved',
    profile_snapshot     JSONB,
    avatar_review_state  TEXT NOT NULL DEFAULT 'approved',
    avatar_prev_key      TEXT NOT NULL DEFAULT '',

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT p_gender_chk  CHECK (gender IS NULL OR gender IN ('M', 'F')),
    CONSTRAINT p_birth_chk   CHECK (birth_ym IS NULL OR birth_ym BETWEEN 190001 AND 299912),
    CONSTRAINT p_height_chk  CHECK (height_cm IS NULL OR height_cm BETWEEN 140 AND 210),
    CONSTRAINT p_weight_chk  CHECK (weight_kg IS NULL OR weight_kg BETWEEN 35 AND 150),
    CONSTRAINT p_edu_chk     CHECK (education_level IS NULL OR education_level BETWEEN 1 AND 4),
    CONSTRAINT p_tier_chk    CHECK (school_tier IS NULL OR school_tier BETWEEN 1 AND 5),
    CONSTRAINT p_income_chk  CHECK (income_band IS NULL OR income_band BETWEEN 1 AND 6),
    CONSTRAINT p_chrono_chk  CHECK (chronotype IS NULL OR chronotype BETWEEN 1 AND 3),
    CONSTRAINT p_smoking_chk CHECK (smoking IS NULL OR smoking BETWEEN 0 AND 2),
    CONSTRAINT p_drinking_chk CHECK (drinking IS NULL OR drinking BETWEEN 0 AND 2),
    CONSTRAINT p_completeness_chk CHECK (completeness BETWEEN 0 AND 100),
    CONSTRAINT p_review_chk CHECK (profile_review_state IN ('approved', 'pending', 'rejected')),
    CONSTRAINT p_avatar_review_chk CHECK (avatar_review_state IN ('approved', 'pending', 'rejected'))
);

-- 候选集主查询：异性 + 同城 + 年龄，列顺序按选择性排列
CREATE INDEX idx_profiles_pool
    ON profiles (gender, city_code, birth_ym, height_cm, education_level);

-- --------------------------------------------------------------- photos
-- 删掉某个用户的照片时，用 ROW_NUMBER 重排 position 会短暂撞唯一约束，
-- 所以那条唯一约束必须是 DEFERRABLE —— 见文件末尾的说明。
CREATE TABLE photos (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT   NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    object_key   TEXT     NOT NULL,
    position     SMALLINT NOT NULL,   -- position = 0 即引荐卡封面，不另设 is_main
    -- review_state 只记录人工巡检进度，不是展示的前置条件：
    -- 图片上传即生效，不阻塞登录与匹配
    review_state TEXT     NOT NULL DEFAULT 'unreviewed',
    reviewed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT photos_position_chk CHECK (position BETWEEN 0 AND 8),
    CONSTRAINT photos_review_chk   CHECK (review_state IN ('unreviewed', 'ok', 'violation')),
    -- DEFERRABLE 的理由：拖拽排序在一个事务里交换两个 position，
    -- 非延迟的唯一约束逐行检查，中间态的重复值必然失败。
    -- 代价是不能用 ON CONFLICT 做冲突推断，新增照片只能事务内「先查再增改」。
    CONSTRAINT photos_user_id_position_key
        UNIQUE (user_id, position) DEFERRABLE INITIALLY DEFERRED
);

-- 运营巡检队列：未看过的新图优先
CREATE INDEX idx_photos_unreviewed ON photos (created_at DESC) WHERE review_state = 'unreviewed';

-- ---------------------------------------------------------- preferences
CREATE TABLE preferences (
    user_id       BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,

    -- 硬条件 5 项，一票否决
    birth_ym_min  INTEGER,
    birth_ym_max  INTEGER,
    city_codes    INTEGER[],          -- NULL = 不限，最多 5 个
    want_child    SMALLINT,           -- 婚育意愿，NULL = 不限
    accept_divorced SMALLINT,         -- 是否接受有婚史，NULL = 不限
    accept_remote   SMALLINT,         -- 异地接受度，NULL = 不限

    -- 软偏好 3 项，参与打分不做过滤，NULL = 不限
    edu_min       SMALLINT,
    height_min    SMALLINT,
    height_max    SMALLINT,
    income_min    SMALLINT,
    income_max    SMALLINT,

    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT prefs_cities_len_chk
        CHECK (city_codes IS NULL OR array_length(city_codes, 1) <= 5)
);

CREATE INDEX idx_prefs_cities ON preferences USING GIN (city_codes);

-- -------------------------------------------------------- introductions
-- 一对用户只占一行，用 user_low < user_high 归一。每一方的状态是行上的列，
-- 不是另一行 —— 这样「双方同时表态」只需锁一行。
CREATE TABLE introductions (
    id            BIGSERIAL PRIMARY KEY,
    batch_id      UUID        NOT NULL,   -- 同一次推送携带的 1–3 条共享

    user_low      BIGINT      NOT NULL REFERENCES users (id),
    user_high     BIGINT      NOT NULL REFERENCES users (id),

    kind          TEXT        NOT NULL,   -- paired | oneway
    state         TEXT        NOT NULL DEFAULT 'pending',

    -- 单向引荐时，hidden_side 那一方从未收到过这条，也永远不参与终结通知
    hidden_side   TEXT,                   -- NULL | 'low' | 'high'

    low_viewed_at  TIMESTAMPTZ,
    high_viewed_at TIMESTAMPTZ,
    low_action     TEXT,                  -- NULL | like | pass
    high_action    TEXT,
    low_action_at  TIMESTAMPTZ,
    high_action_at TIMESTAMPTZ,
    low_reason     TEXT,                  -- 不合适的三个一键选项
    high_reason    TEXT,

    -- 打分留痕，供后续调参与 AI 训练
    score_low_to_high REAL,
    score_high_to_low REAL,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    closed_at     TIMESTAMPTZ,

    CONSTRAINT intro_kind_chk  CHECK (kind IN ('paired', 'oneway')),
    CONSTRAINT intro_state_chk CHECK (
        state IN ('pending', 'viewed', 'responded', 'matched', 'declined', 'expired')
    ),
    CONSTRAINT intro_order_chk CHECK (user_low < user_high),
    CONSTRAINT intro_hidden_chk CHECK (hidden_side IS NULL OR hidden_side IN ('low', 'high')),
    CONSTRAINT intro_action_chk CHECK (
        (low_action IS NULL OR low_action IN ('like', 'pass')) AND
        (high_action IS NULL OR high_action IN ('like', 'pass'))
    ),
    -- 单向引荐必须有隐藏方，成对引荐必须没有
    CONSTRAINT intro_hidden_kind_chk CHECK (
        (kind = 'oneway' AND hidden_side IS NOT NULL) OR
        (kind = 'paired' AND hidden_side IS NULL)
    )
);

-- 载重约束：同一对用户同时只能有一条「未终结」的引荐。
-- 它让「单向升级为成对」必须走 UPDATE 而不是 INSERT —— 见 §13.3。
-- 删掉它，同一对用户会被反复推送。
CREATE UNIQUE INDEX uniq_intro_open_pair
    ON introductions (user_low, user_high)
    WHERE state IN ('pending', 'viewed', 'responded');

-- 超时扫描：只扫未终结的
CREATE INDEX idx_intro_expire
    ON introductions (expires_at)
    WHERE state IN ('pending', 'viewed', 'responded');

-- 「我的引荐列表」两条，按 low / high 两个方向查
CREATE INDEX idx_intro_low  ON introductions (user_low,  created_at DESC);
CREATE INDEX idx_intro_high ON introductions (user_high, created_at DESC);

-- ---------------------------------------------------- push_subscriptions
CREATE TABLE push_subscriptions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- endpoint 是载重唯一键：同一个浏览器订阅会被同一用户反复注册，
    -- 也会被不同用户注册（换账号登录）。唯一键让注册变成 upsert，
    -- 并且切换账号时旧订阅自动归新用户所有。
    endpoint    TEXT   NOT NULL UNIQUE,
    p256dh      TEXT   NOT NULL,
    auth        TEXT   NOT NULL,
    user_agent  TEXT   NOT NULL DEFAULT '',
    fail_count  SMALLINT NOT NULL DEFAULT 0,
    last_error  TEXT   NOT NULL DEFAULT '',
    disabled_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT push_endpoint_len_chk CHECK (char_length(endpoint) <= 1024)
);

-- 投递时只取该用户仍有效的订阅
CREATE INDEX idx_push_active
    ON push_subscriptions (user_id) WHERE disabled_at IS NULL;

-- --------------------------------------------------------------- outbox
-- 所有外发通知的唯一出口。加一个 channel 列，V1.1 补邮件通道时
-- 只是多一个 channel='email' 的取值，不用新建表。
CREATE TABLE outbox (
    id            BIGSERIAL PRIMARY KEY,
    channel       TEXT NOT NULL,          -- push | email（email 留到 V1.1）
    user_id       BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    template      TEXT NOT NULL,          -- intro_delivered | intro_missed | intro_closed | matched
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 形如 intro:123:low:delivered。幂等的全部实现：重试、并发、
    -- worker 重启都不会让同一个人收到重复推送。
    dedup_key     TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'pending',
    attempts      SMALLINT NOT NULL DEFAULT 0,
    last_error    TEXT NOT NULL DEFAULT '',
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT outbox_status_chk CHECK (status IN ('pending', 'sent', 'failed'))
);

CREATE INDEX idx_outbox_pending
    ON outbox (next_retry_at) WHERE status = 'pending';

-- -------------------------------------------------------------- matches
CREATE TABLE matches (
    id          BIGSERIAL PRIMARY KEY,
    user_low    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    user_high   BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status      TEXT   NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_msg_at TIMESTAMPTZ,
    -- 这条唯一约束是「同一对用户只有一条匹配」的唯一保证，
    -- 也是双方同时表态这个竞态的解法。不要删。
    UNIQUE (user_low, user_high),
    CONSTRAINT matches_order_chk  CHECK (user_low < user_high),
    CONSTRAINT matches_status_chk CHECK (status IN ('active', 'blocked', 'closed'))
);

CREATE INDEX idx_matches_low  ON matches (user_low,  last_msg_at DESC NULLS LAST);
CREATE INDEX idx_matches_high ON matches (user_high, last_msg_at DESC NULLS LAST);

-- ------------------------------------------------------------- messages
CREATE TABLE messages (
    id            BIGSERIAL PRIMARY KEY,
    match_id      BIGINT NOT NULL REFERENCES matches (id) ON DELETE CASCADE,
    sender_id     BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    content       TEXT   NOT NULL,
    client_msg_id TEXT   NOT NULL,   -- 客户端生成，保证发送幂等
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (match_id, client_msg_id),
    CONSTRAINT messages_content_chk CHECK (char_length(content) BETWEEN 1 AND 1000)
);

CREATE INDEX idx_messages_match ON messages (match_id, id DESC);  -- 游标分页

-- ---------------------------------------------------------- match_reads
CREATE TABLE match_reads (
    match_id         BIGINT NOT NULL REFERENCES matches (id) ON DELETE CASCADE,
    user_id          BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    last_read_msg_id BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (match_id, user_id)
);

-- --------------------------------------------------------------- blocks
CREATE TABLE blocks (
    blocker_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_no_self_chk CHECK (blocker_id <> blocked_id)
);

CREATE INDEX idx_blocks_reverse ON blocks (blocked_id);  -- 候选集双向排除

-- -------------------------------------------------------------- reports
CREATE TABLE reports (
    id           BIGSERIAL PRIMARY KEY,
    reporter_id  BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reported_id  BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason       TEXT   NOT NULL,
    detail       TEXT   NOT NULL DEFAULT '',
    evidence_key TEXT   NOT NULL DEFAULT '',
    status       TEXT   NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT reports_reason_chk CHECK (reason IN ('fake', 'harassment', 'porn', 'ad_fraud', 'other')),
    CONSTRAINT reports_status_chk CHECK (status IN ('pending', 'valid', 'invalid')),
    CONSTRAINT reports_no_self_chk CHECK (reporter_id <> reported_id)
);

CREATE INDEX idx_reports_pending  ON reports (created_at DESC) WHERE status = 'pending';
CREATE INDEX idx_reports_reported ON reports (reported_id, status);

-- -------------------------------------------------------- admin_actions
-- 人工审核必须可追溯，否则误封无法复盘
CREATE TABLE admin_actions (
    id             BIGSERIAL PRIMARY KEY,
    operator       TEXT   NOT NULL,
    target_user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action         TEXT   NOT NULL,
    reason         TEXT   NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_actions_target ON admin_actions (target_user_id, created_at DESC);

-- ------------------------------------------------------------ job_runs
-- 定时任务的租约表。抢租约是一条条件 UPDATE（leased_until < now()），
-- 见 §15.1。租约只防重复劳动，不保证正确性 —— 正确性来自
-- uniq_intro_open_pair。
CREATE TABLE job_runs (
    name         TEXT PRIMARY KEY,
    leased_until TIMESTAMPTZ NOT NULL DEFAULT to_timestamp(0),
    last_run_at  TIMESTAMPTZ,
    last_result  TEXT NOT NULL DEFAULT ''
);

-- 两个需要租约的循环。outbox 投递是 2 秒轮询，靠 FOR UPDATE SKIP LOCKED
-- 天然互斥，不需要租约，所以不在这里建行。
INSERT INTO job_runs (name) VALUES ('intro_generate'), ('intro_expire');

-- -------------------------------------------------------- user_settings
CREATE TABLE user_settings (
    user_id         BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    intros_paused   BOOLEAN NOT NULL DEFAULT false,
    paused_at       TIMESTAMPTZ,          -- 暂停期间被引荐，计时从这里冻结
    quiet_start     SMALLINT NOT NULL DEFAULT 22,
    quiet_end       SMALLINT NOT NULL DEFAULT 9,
    push_frozen     BOOLEAN NOT NULL DEFAULT false,  -- 连续 2 次推送未打开
    unopened_streak SMALLINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT settings_quiet_chk CHECK (
        quiet_start BETWEEN 0 AND 23 AND quiet_end BETWEEN 0 AND 23
    )
);
