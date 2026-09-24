-- 把作息加回来。列能回来，值回不来 —— 000004 的 up 是有意丢掉那些值的
-- （见那个文件的说明），所以这里只能补一个整列 NULL 的字段。
--
-- 约束与 000001_init.up.sql 里的那句逐字一致，回滚之后 schema 与
-- 000003 结束时完全相同。
ALTER TABLE profiles ADD COLUMN chronotype SMALLINT;

ALTER TABLE profiles
    ADD CONSTRAINT p_chrono_chk CHECK (chronotype IS NULL OR chronotype BETWEEN 1 AND 3);
