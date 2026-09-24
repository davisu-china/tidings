package schools

import "testing"

func TestTierOf(t *testing.T) {
	cases := []struct {
		name string
		want int16
	}{
		// 国内 QS 前 500 → Tier 5
		{"清华大学", TierQS500},
		{"北京大学", TierQS500},
		{"浙江大学", TierQS500},
		{"哈尔滨工业大学", TierQS500},
		{"武汉大学", TierQS500},
		// 985（非 QS 前 500）→ Tier 4
		{"西北农林科技大学", Tier985},
		{"中央民族大学", Tier985},
		// 211 → Tier 3
		{"北京邮电大学", TierKey},
		{"苏州大学", TierKey},
		// 普通本科 → Tier 2
		{"杭州师范大学", TierOrdinary},
		// 专科 → Tier 1
		{"浙江金融职业学院", TierOther},

		// 别名
		{"清华", TierQS500},
		{"北大", TierQS500},
		{"哈工大", TierQS500},
		{"北邮", TierKey},
		{"MIT", TierQS500},
		{"mit", TierQS500},

		// 大小写与空格不敏感
		{"  清华大学 ", TierQS500},

		// 带院系后缀走后缀去重
		{"浙江大学计算机学院", TierQS500},

		// 未收录归到普通本科而不是最低档，宁可放宽不可错杀
		{"某某职业技术学院", TierOrdinary},
		{"完全不存在的大学", TierOrdinary},

		// 空值是真的没填
		{"", TierOther},
		{"   ", TierOther},
	}

	for _, tt := range cases {
		if got := TierOf(tt.name); got != tt.want {
			t.Errorf("TierOf(%q) = %d，期望 %d", tt.name, got, tt.want)
		}
	}
}

func TestSearch(t *testing.T) {
	if got := Search("清华", 10); len(got) == 0 || got[0].Name != "清华大学" {
		t.Errorf("按中文名搜索失败: %v", got)
	}
	if got := Search("qinghua", 10); len(got) == 0 || got[0].Name != "清华大学" {
		t.Errorf("按全拼搜索失败: %v", got)
	}
	if got := Search("bjyddx", 10); len(got) == 0 || got[0].Name != "北京邮电大学" {
		t.Errorf("按拼音缩写搜索失败: %v", got)
	}
	if got := Search("北京", 30); len(got) < 5 {
		t.Errorf("模糊搜索「北京」结果过少: %d 条", len(got))
	}

	// limit 必须生效，否则联想框会被刷爆
	if got := Search("大学", 3); len(got) != 3 {
		t.Errorf("limit 未生效，返回 %d 条", len(got))
	}
	// 空查询返回空数组而不是 nil，前端可以直接遍历
	if got := Search("", 10); got == nil || len(got) != 0 {
		t.Errorf("空查询应返回空数组，得到 %v", got)
	}
	if got := Search("清华", 0); len(got) != 0 {
		t.Errorf("limit=0 应返回空，得到 %v", got)
	}
}

// 别名必须能搜到。人不会打全称，联想框里敲的就是「北大」「交大」这些 ——
// 上游那份实现只让 TierOf 查别名，Search 不查，等于联想框在最常用的
// 那几个词上全是空的。
func TestSearchByAlias(t *testing.T) {
	cases := []struct {
		q    string
		want string
	}{
		{"北大", "北京大学"},
		{"清华", "清华大学"},
		{"交大", "上海交通大学"},
		{"MIT", "麻省理工学院"},
		{"mit", "麻省理工学院"},
		{"Tokyo", "东京大学"},
	}

	for _, tt := range cases {
		got := Search(tt.q, 10)
		found := false
		for _, r := range got {
			if r.Name == tt.want {
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(got))
			for _, r := range got {
				names = append(names, r.Name)
			}
			t.Errorf("搜 %q 没找到 %s，返回了 %v", tt.q, tt.want, names)
		}
	}
}

// 搜「北大」第一条得是北京大学。这里真正想拦的是「东北大学」：
// 它四个字里从第 2 个起就含着「北大」，只按子串位置排的话它会顶到最前。
func TestSearchAliasExactBeatsNameSubstring(t *testing.T) {
	got := Search("北大", 10)
	if len(got) == 0 {
		t.Fatal("搜「北大」没有结果")
	}
	if got[0].Name != "北京大学" {
		t.Errorf("第一条应是北京大学，得到 %s（完整结果 %v）", got[0].Name, got)
	}
}

// 别名的排位规则：整条相等算满分，子串命中压到校名命中之后
func TestAliasRank(t *testing.T) {
	pinyinMu.Lock()
	byName := buildCandidates(School{Name: "北大附中", Tier: TierOrdinary})
	byAlias := buildCandidates(School{Name: "某某大学", Tier: TierQS500, Alias: []string{"北大荒分校"}})
	pinyinMu.Unlock()

	// 校名里带「北大」的，落在前缀位置
	if got := bestIndex(byName.name, "北大"); got != 0 {
		t.Fatalf("「北大附中」按校名应命中在 0，得到 %d", got)
	}
	// 别名只沾个边（子串），必须排到正数位，也就是所有校名命中之后
	if got := aliasRank(byAlias.alias, "北大"); got <= 0 {
		t.Errorf("别名的子串命中排位应为正数，得到 %d", got)
	}
	// 别名整条相等是满分
	if got := aliasRank(byAlias.alias, "北大荒分校"); got != 0 {
		t.Errorf("别名完全相等应为 0，得到 %d", got)
	}
	if got := aliasRank(byAlias.alias, "查无此校"); got != -1 {
		t.Errorf("没命中应为 -1，得到 %d", got)
	}
}

// 别名不覆盖正式校名：「东大」既指东南大学也指东京大学，先写入者优先
func TestAliasDoesNotOverrideOfficialName(t *testing.T) {
	if got := TierOf("东南大学"); got != TierQS500 {
		t.Errorf("东南大学应为 QS500，得到 %d", got)
	}
	if got := TierOf("东京大学"); got != TierQS500 {
		t.Errorf("东京大学应为 QS500，得到 %d", got)
	}
}

func TestCount(t *testing.T) {
	// 教育部全量名单 + QS 前 500，应远超 3000
	if n := Count(); n < 3000 {
		t.Errorf("院校库只加载到 %d 条，数据可能有问题", n)
	}
}

// 层级必须落在 1..5，越界会让 SQL 的范围筛选出现意外结果
func TestAllTiersInRange(t *testing.T) {
	load()
	for _, s := range all {
		if s.Tier < TierOther || s.Tier > TierQS500 {
			t.Errorf("%s 的层级 %d 越界", s.Name, s.Tier)
		}
	}
}

func TestNoDuplicateNames(t *testing.T) {
	load()
	seen := make(map[string]bool, len(all))
	for _, s := range all {
		if seen[s.Name] {
			t.Errorf("院校库中有重复条目: %s", s.Name)
		}
		seen[s.Name] = true
	}
}

// 拼音缓存：同一校名多次调用返回一致结果
func TestPinyinCache(t *testing.T) {
	f1, a1 := pinyinOf("清华大学")
	f2, a2 := pinyinOf("清华大学")
	if f1 != f2 || a1 != a2 {
		t.Error("拼音缓存结果不一致")
	}
	if f1 == "" || a1 == "" {
		t.Error("拼音不应为空")
	}
}
