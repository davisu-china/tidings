-- 回滚会丢掉这两列里的取值。这是有意的：它们的唯一来源是建档向导，
-- 用户重新填一遍即可，不值得为它们写一套备份/恢复。
--
-- 先删约束再删列：列没了约束本来也会一起没，但显式写出来，
-- 回滚脚本才读得懂自己删了什么。

ALTER TABLE profiles
    DROP CONSTRAINT p_want_child_chk,
    DROP CONSTRAINT p_marital_chk;

ALTER TABLE profiles
    DROP COLUMN want_child,
    DROP COLUMN marital_status;
