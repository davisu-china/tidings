package password

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// testCost 用最低档跑测试。生产是 12（约 200–300ms），
// 一整套测试跑下来会平白多等好几秒。
const testCost = bcrypt.MinCost

func TestHashVerifyRoundTrip(t *testing.T) {
	const plain = "correct horse battery staple"
	h, err := Hash(plain, testCost)
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	if h == plain {
		t.Fatal("哈希结果不能等于原文")
	}
	if !Verify(h, plain) {
		t.Fatal("正确密码应当通过校验")
	}
	if Verify(h, plain+"x") {
		t.Fatal("错误密码不该通过校验")
	}
}

// TestHashAlwaysSalted 保证同一密码两次哈希不同 ——
// 否则相同密码在库里长得一样，一次泄露就能看出谁和谁密码相同。
func TestHashAlwaysSalted(t *testing.T) {
	a, err := Hash("same-password", testCost)
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	b, err := Hash("same-password", testCost)
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	if a == b {
		t.Fatal("两次哈希结果相同，说明没有加盐")
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		plain   string
		minLen  int
		wantErr error
	}{
		{"空密码", "", 8, ErrEmpty},
		{"太短", "abc12345"[:7], 8, ErrTooShort},
		{"刚好够长", "abc12345", 8, nil},
		// 下限按字符数算：8 个汉字够长（用户的口径）
		{"八个汉字", strings.Repeat("密", 8), 8, nil},
		// 上限按字节数算：25 个汉字 = 75 字节，超过 bcrypt 的截断点
		{"超过 72 字节", strings.Repeat("密", 25), 8, ErrTooLong},
		{"正好 72 字节", strings.Repeat("a", 72), 8, nil},
		{"73 字节", strings.Repeat("a", 73), 8, ErrTooLong},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.plain, tc.minLen)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("得到 %v，期望 %v", err, tc.wantErr)
			}
		})
	}
}

// TestHashRejectsOverlongInput 是关键一条：bcrypt 只取前 72 字节，
// 超出部分被静默丢弃。不报错的话，用户以为自己设了更长的密码，其实没有。
func TestHashRejectsOverlongInput(t *testing.T) {
	if _, err := Hash(strings.Repeat("a", 73), testCost); !errors.Is(err, ErrTooLong) {
		t.Fatalf("超过 72 字节应当报错，得到 %v", err)
	}
}

// TestEqualizerDummyIsRealHash 挡住一个我自己踩过的坑。
//
// 假哈希必须是真生成出来的。写死一个形似 bcrypt 的字符串的话，
// CompareHashAndPassword 会在解析阶段就失败并立刻返回 ——
// 比真实比对还快，等于把「邮箱不存在」这条分支的时序差放大了，
// 而它存在的唯一目的就是把这条分支抹平。
func TestEqualizerDummyIsRealHash(t *testing.T) {
	eq, err := NewEqualizer(testCost)
	if err != nil {
		t.Fatalf("构造 Equalizer 失败: %v", err)
	}

	// 能解析出 cost 与盐，说明它是真哈希而不是一段随便的字符串
	cost, err := bcrypt.Cost(eq.dummy)
	if err != nil {
		t.Fatalf("假哈希无法解析，会走快速失败路径: %v", err)
	}
	if cost != testCost {
		t.Fatalf("假哈希的 cost = %d，期望与真实哈希一致的 %d", cost, testCost)
	}

	// 与真实比对一样返回「不匹配」，而不是「哈希无效」
	err = bcrypt.CompareHashAndPassword(eq.dummy, []byte("anything"))
	if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("假哈希应返回不匹配，得到 %v", err)
	}
}

func TestEqualizerBurnAlwaysFalse(t *testing.T) {
	eq, err := NewEqualizer(testCost)
	if err != nil {
		t.Fatalf("构造 Equalizer 失败: %v", err)
	}
	for _, in := range []string{"", "guess", strings.Repeat("x", 100)} {
		if eq.Burn(in) {
			t.Fatalf("%q 不该通过假哈希比对", in)
		}
	}

	// nil 接收者不能 panic：装配漏了的时候应当是「少一层防护」，不是崩溃
	var nilEq *Equalizer
	if nilEq.Burn("x") {
		t.Fatal("nil Equalizer 不该返回 true")
	}
}
