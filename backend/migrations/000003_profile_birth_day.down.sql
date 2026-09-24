-- 先删约束再删列。直接 DROP COLUMN 会连约束一起带走，但留着这句
-- 回滚路径更明确：约束是被显式删掉的，不是顺带消失的。
ALTER TABLE profiles DROP CONSTRAINT p_birth_day_chk;
ALTER TABLE profiles DROP COLUMN birth_day;
