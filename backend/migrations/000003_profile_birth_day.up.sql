-- 出生日期精确到「日」。
--
-- 建档页的出生日期是「年 → 月 → 日」三级联动，日必须落库，否则用户选了
-- 等于没选。
--
-- 为什么是加一列，而不是把 birth_ym 换成 DATE / YYYYMMDD：birth_ym 这个
-- YYYYMM 编码被四样东西依赖 ——
--   1. preferences.birth_ym_min/max 的岁↔月换算（service/preference.go），
--      绝对值被 preference_test.go 钉住；
--   2. 候选集 SQL 的四个年龄占位参数与全部 p.birth_ym/pref.birth_ym_min
--      列名（repo/matching.go）；
--   3. idx_profiles_pool 的第三列；
--   4. p_birth_chk 约束。
-- 换编码要同时改这四处，还有 backend/scripts 下几处硬编码 birth_ym 的脚本。
-- 加一列则它们一处都不用动。
--
-- 老数据（只知道年月）的 birth_day 留 NULL：不知道就是不知道，不编造。
-- NULL 不参与年龄计算与候选集 —— 年龄始终是月粒度（见 service 的
-- ageFromBirthYM），birth_day 只影响展示，不进完整度、不进入池门槛。

ALTER TABLE profiles ADD COLUMN birth_day SMALLINT;

-- 日的合法性依赖年月（闰年 2 月 29、小月 30），所以这条约束必须连年月一起看。
-- 把整张日历写进库里，是因为 service 之上还有手工 SQL 与后台改数。
--
-- 用 CASE 而不是 make_date()：make_date 遇到非法日期是**抛错**（22008），
-- 在 CHECK 里会变成一句与约束无关的报错，而不是「违反 p_birth_day_chk」。
-- PG 的整数除法在这里正好够用（birth_ym 已被 p_birth_chk 限定为正数）。
ALTER TABLE profiles
    ADD CONSTRAINT p_birth_day_chk CHECK (
        birth_day IS NULL OR (
            birth_ym IS NOT NULL
            AND birth_day >= 1
            AND birth_day <= CASE birth_ym % 100
                WHEN 2 THEN CASE
                    WHEN (birth_ym / 100) % 4 = 0
                     AND ((birth_ym / 100) % 100 <> 0 OR (birth_ym / 100) % 400 = 0)
                    THEN 29 ELSE 28 END
                WHEN 4 THEN 30 WHEN 6 THEN 30 WHEN 9 THEN 30 WHEN 11 THEN 30
                ELSE 31 END
        )
    );

-- 不建索引：这一列永远不进任何 WHERE，只跟着整行一起读出来。
