package migrations

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
)

var nameRe = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.(up|down)\.sql$`)

// TestMigrationFiles 检查文件命名与成对性。
//
// 这是没有数据库时唯一能验的东西，但它挡的是真会发生的错：
// 少一个 .down.sql、版本号不连续，golang-migrate 只会在启动时
// 抛一句难懂的 error。写在这里，让它在 go test 阶段就失败。
func TestMigrationFiles(t *testing.T) {
	src, err := iofs.New(FS, ".")
	if err != nil {
		t.Fatalf("迁移文件无法被 golang-migrate 读取: %v", err)
	}
	defer src.Close()

	first, err := src.First()
	if err != nil {
		t.Fatalf("读取迁移列表失败: %v", err)
	}

	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("读取嵌入目录失败: %v", err)
	}

	type pair struct{ up, down bool }
	versions := map[uint64]*pair{}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := nameRe.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("迁移文件名不符合 {version}_{name}.{up|down}.sql: %s", e.Name())
			continue
		}
		v, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			t.Errorf("版本号无法解析: %s", e.Name())
			continue
		}
		if versions[v] == nil {
			versions[v] = &pair{}
		}
		if m[3] == "up" {
			versions[v].up = true
		} else {
			versions[v].down = true
		}
	}

	if len(versions) == 0 {
		t.Fatal("一个迁移文件都没有")
	}

	var nums []uint64
	for v := range versions {
		nums = append(nums, v)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })

	// 版本号必须从 1 开始连续，中间断开说明有文件没提交进来
	for i, v := range nums {
		if want := uint64(i + 1); v != want {
			t.Errorf("版本号不连续：第 %d 个是 %d，应为 %d", i+1, v, want)
		}
		if !versions[v].up {
			t.Errorf("版本 %d 缺少 .up.sql", v)
		}
		if !versions[v].down {
			t.Errorf("版本 %d 缺少 .down.sql", v)
		}
	}

	// 首个版本号必须是 1
	if nums[0] != 1 {
		t.Errorf("首个版本号是 %d，应为 1", nums[0])
	}

	// 用 golang-migrate 自己的解析器走一遍，确认它认得这些文件。
	// 它认不出时不会报错，只会认为「一个迁移都没有」—— 那样 api
	// 启动时会静默跳过建表，是最难查的一类故障。
	if uint64(first) != nums[0] {
		t.Errorf("golang-migrate 的首个版本是 %d，与文件名解析结果 %d 不一致", first, nums[0])
	}
	// 顺着游标走到底，确认每一版它都认得
	var walked []uint64
	for v := first; ; {
		walked = append(walked, uint64(v))
		next, err := src.Next(v)
		if err != nil {
			break
		}
		v = next
	}
	if len(walked) != len(nums) {
		t.Errorf("golang-migrate 只解析出 %d 个版本 %v，文件名里有 %d 个 %v",
			len(walked), walked, len(nums), nums)
	}
}
