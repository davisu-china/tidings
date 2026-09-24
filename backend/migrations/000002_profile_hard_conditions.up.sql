-- 给 profiles 补两个硬条件字段。
--
-- 为什么要有这条迁移：§4.3 把「婚育意愿 / 婚史 / 异地接受度」列为硬条件，
-- §12.1 的第二步要求逐条比对，但 §10.1 的 profiles 里只有 preferences 侧
-- 有对应列 —— 那是「我要求对方怎样」，没有「我自己是什么情况」可比，
-- 过滤根本无从下手。
--
-- 只补两项，第三项（异地接受度）**不补**：preferences.accept_remote 已经
-- 存在，问的就是同一个问题（「我接不接受异地」）。在两个表里各存一份
-- 会立刻产生一个没有答案的问题 —— 两处取值不一致时听谁的？异地接受度
-- 也不是「关于我的事实」（像婚史那样），而是「我对关系形态的要求」，
-- 本来就属于偏好侧。所以过滤一律读 preferences.accept_remote。
--
-- 留下的两项是真缺的，且都是「关于我的事实」：
--   want_child     我与 preferences.want_child 对着比，判「想要 vs 不要」的冲突
--   marital_status 我与 preferences.accept_divorced 对着比，判对方接不接受我
--
-- 两项都随建档一起填，可空（NULL = 未填，硬条件里按「不限」处理，
-- 与 preferences 的「不限」三处协同保持一致）。
--
-- 取值约定（小整数，与 profiles 里既有的 education_level 等一致）：
--   want_child     1 想要孩子 / 2 不要孩子 / 3 再说
--   marital_status 1 未婚     / 2 离异

ALTER TABLE profiles
    ADD COLUMN want_child      SMALLINT,
    ADD COLUMN marital_status  SMALLINT;

-- 分开写而不是塞进已有的 p_*_chk：ALTER TABLE ADD CONSTRAINT 一次只能加一条，
-- 而且这样回滚时每条约束的来历是清楚的。
ALTER TABLE profiles
    ADD CONSTRAINT p_want_child_chk
        CHECK (want_child IS NULL OR want_child BETWEEN 1 AND 3),
    ADD CONSTRAINT p_marital_chk
        CHECK (marital_status IS NULL OR marital_status BETWEEN 1 AND 2);

-- 婚育意愿和婚史是候选集硬过滤里最常用的两项。不单独建索引：
-- 它们跟在 idx_profiles_pool 的同城/年龄筛选之后，选择性远不如那几列，
-- 再加一条索引只会拖慢写入。等线上 EXPLAIN 说需要时再补。
