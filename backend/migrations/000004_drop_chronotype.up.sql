-- 删掉作息（chronotype）。
--
-- 它不是「少一个字段」，而是一条会反向激励的规则。作息只喂
-- service/match.go 的 lifestyleAffinity，而那条规则是「任一方未填就不计入
-- 分母」——三项里只填吸烟和饮酒的人，生活方式分由两项算；三项全不填的人
-- 反而直接拿满分 1.0。也就是说：**如实填一项不如不填**。作息只有这一处
-- 用途，删掉它，这个洞在剩下的两项上顺带补上（吸烟与饮酒同时转为必填，
-- 见 service/profile.go 的 requiredMissing）。
--
-- 数据丢失是有意的，也没有留备份路径：作息不进任何筛选、不进任何界面
-- 列表、不被导出，历史值唯一的去处就是那个已经删掉的分母。留一份
-- 「作息归档表」等于留一列永远没人读的数据。
--
-- 先删约束再删列。直接 DROP COLUMN 会连约束一起带走，但留着这句
-- 回滚路径更明确：约束是被显式删掉的，不是顺带消失的。
ALTER TABLE profiles DROP CONSTRAINT p_chrono_chk;
ALTER TABLE profiles DROP COLUMN chronotype;
