// Package schools 提供院校库：联想搜索 + 层级归一。
//
// 数据来自 stubborn-love 那套已经跑过的实现（scripts/gen_schools.py 生成
// schools.json）：教育部全国高等学校名单（3004 所，含本科与专科）+
// QS 前 500 海外高校，当前 3141 条。拼音不落在数据里，由 go-pinyin
// 在运行时生成并缓存 —— 3000+ 所学校手工维护拼音不现实。
//
// 两个用途：
//   - 建档页的「毕业院校」不再让人自由输入，走 Search 做联想（只列库里的）；
//   - 保存档案时按校名归一出 school_tier，存进 profiles.school_tier。
//     school_name 只用于展示，参与筛选与打分的是 tier。
package schools

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/mozillazg/go-pinyin"
)

// 院校层级，与 profiles.school_tier 对应。
const (
	TierOther    int16 = 1 // 专科 / 其他
	TierOrdinary int16 = 2 // 普通本科（也是匹配不到时的默认值）
	TierKey      int16 = 3 // 211 / 双一流
	Tier985      int16 = 4 // 985
	TierQS500    int16 = 5 // QS 前 500（含海外顶尖高校）
)

//go:embed schools.json
var rawJSON []byte

type School struct {
	Name  string   `json:"n"`
	Tier  int16    `json:"t"`
	Alias []string `json:"alias,omitempty"`
}

// Result 是搜索返回项。
type Result struct {
	Name string `json:"name"`
	Tier int16  `json:"tier"`
}

// searchable 是一所学校的可匹配串，分两组：
// name 是正式校名及其拼音，alias 是别名及其拼音。
// 分组是为了让「校名命中」排在「别名命中」前面（见 Search）。
type searchable struct {
	name  []string
	alias []string
}

/** 别名命中的排位罚分：同一所学校，校名里出现的总比别名里出现的更相关。 */
const aliasPenalty = 100

var (
	once   sync.Once
	all    []School
	cands  []searchable
	byName map[string]int16
	// 拼音缓存：name -> 全拼 / 首字母缩写
	pinyinMu sync.Mutex
	fullPy   = map[string]string{}
	abbrPy   = map[string]string{}
)

func load() {
	once.Do(func() {
		if err := json.Unmarshal(rawJSON, &all); err != nil {
			panic("加载院校库失败: " + err.Error())
		}
		byName = make(map[string]int16, len(all)*2)
		for _, s := range all {
			byName[normalize(s.Name)] = s.Tier
			// 别名不覆盖正式校名：「东大」既指东南大学也指东京大学，
			// 先写入者优先，正式名永远赢
			for _, a := range s.Alias {
				if _, exists := byName[normalize(a)]; !exists {
					byName[normalize(a)] = s.Tier
				}
			}
		}
		// 候选串在这里一次性算好。Search 每敲一个字都要把 3141 所学校
		// 全过一遍，把拼音留在里面现算的话，联想框每一下按键都要卡一下。
		pinyinMu.Lock()
		cands = make([]searchable, len(all))
		for i, s := range all {
			cands[i] = buildCandidates(s)
		}
		pinyinMu.Unlock()
	})
}

// buildCandidates 把一所学校摊成可匹配串：本体 + 全拼 + 首字母缩写。
// 调用方需持有 pinyinMu。
func buildCandidates(s School) searchable {
	expand := func(v string) []string {
		n := normalize(v)
		if n == "" {
			return nil
		}
		out := []string{n}
		full, abbr := pinyinOfLocked(v)
		if full != "" {
			out = append(out, full)
		}
		if abbr != "" {
			out = append(out, abbr)
		}
		return out
	}

	c := searchable{name: expand(s.Name)}
	for _, a := range s.Alias {
		c.alias = append(c.alias, expand(a)...)
	}
	return c
}

// TierOf 返回校名对应的层级。匹配不到时返回 TierOrdinary，理由见包注释。
func TierOf(name string) int16 {
	load()
	name = strings.TrimSpace(name)
	if name == "" {
		return TierOther
	}
	n := normalize(name)
	if t, ok := byName[n]; ok {
		return t
	}
	// 用户可能填了「XX大学计算机学院」这类带院系后缀的写法，
	// 尝试去掉常见后缀后匹配
	for _, suffix := range []string{"计算机学院", "软件学院", "医学院", "法学院", "商学院", "学院", "大学", "学校"} {
		if strings.HasSuffix(n, suffix) {
			if t, ok := byName[strings.TrimSuffix(n, suffix)]; ok {
				return t
			}
		}
	}
	return TierOrdinary
}

// Search 做联想匹配：中文名、全拼、拼音首字母缩写，以及别名。
//
// 别名必须查 —— 人不会打全称。「北大」「交大」「MIT」「tokyo」这几个
// 查不到的话，联想框在真实使用里等于半个残废（TierOf 一直是用别名的，
// 只有 Search 漏了）。
//
// 结果按「匹配位置越靠前越优先、层级越高越优先」排序。别名整条相等的
// 算满分，别名的其他命中压到校名命中之后 —— 规则见 aliasRank。
func Search(q string, limit int) []Result {
	load()
	q = normalize(strings.TrimSpace(q))
	if q == "" || limit <= 0 {
		return []Result{}
	}

	type scored struct {
		Result
		rank int
	}
	var hits []scored

	for i, s := range all {
		best := bestIndex(cands[i].name, q)
		if a := aliasRank(cands[i].alias, q); a >= 0 && (best < 0 || a < best) {
			best = a
		}
		if best >= 0 {
			hits = append(hits, scored{Result{Name: s.Name, Tier: s.Tier}, best})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].Tier > hits[j].Tier
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Result, len(hits))
	for i, h := range hits {
		out[i] = h.Result
	}
	return out
}

// bestIndex 返回 q 在候选串里最靠前的一次出现，没出现返回 -1。
func bestIndex(cands []string, q string) int {
	best := -1
	for _, cand := range cands {
		if idx := strings.Index(cand, q); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

// aliasRank 给别名组的命中排位：完全相等算 0（满分），子串命中按
// aliasPenalty 压到校名命中之后。
//
// 为什么完全相等要单独拿出来：中文的子串匹配很粗。「北大」是
// 「东北大学」的第 2、3 个字，也是「西北大学」「河北大学」的子串 ——
// 一律按位置排的话，搜「北大」的第一条会是东北大学，而人想找的
// 北京大学要翻好几屏。别名整条相等是「就是它」的信号，不能当成子串算。
func aliasRank(cands []string, q string) int {
	best := -1
	for _, cand := range cands {
		if cand == q {
			return 0
		}
		if idx := strings.Index(cand, q); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if best < 0 {
		return -1
	}
	return aliasPenalty + best
}

// Count 返回库容量，供健康检查与运维确认数据是否加载成功。
func Count() int {
	load()
	return len(all)
}

// pinyinOf 返回校名的全拼与首字母缩写（带缓存）。
func pinyinOf(name string) (string, string) {
	pinyinMu.Lock()
	defer pinyinMu.Unlock()
	return pinyinOfLocked(name)
}

// pinyinOfLocked 同上，但由调用方持锁 —— 建候选串时要成批调用，
// 逐条加锁会在 load 里锁上加锁。
func pinyinOfLocked(name string) (string, string) {
	if full, ok := fullPy[name]; ok {
		return full, abbrPy[name]
	}

	var fullParts, abbrParts []string
	for _, r := range name {
		if r < 128 { // ASCII（如 MIT、ABC 学院）直接保留
			fullParts = append(fullParts, string(r))
			abbrParts = append(abbrParts, strings.ToLower(string(r)))
			continue
		}
		py := pinyin.LazyPinyin(string(r), pinyin.NewArgs())
		if len(py) > 0 && py[0] != "" {
			p := py[0]
			fullParts = append(fullParts, p)
			abbrParts = append(abbrParts, p[:1])
		}
	}
	full := strings.Join(fullParts, "")
	abbr := strings.Join(abbrParts, "")
	fullPy[name] = full
	abbrPy[name] = abbr
	return full, abbr
}

func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
}
