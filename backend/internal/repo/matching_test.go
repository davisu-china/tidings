package repo

import (
	"testing"
)

// city_codes 是一列 integer[]，读写都得绕过 GORM 的默认行为
// （见 CityCodes 的注释）。这里盯的是三件事：
//
//  1. 两边能不能对上 —— 写进去再读回来还是原来那几个城市。
//  2. 空到底写成什么 —— NULL 还是 `{}`。差一个字符，语义就从
//     「不限城市」翻成「哪个城市都不行」，而且不会报错，只会让人
//     再也收不到引荐。这是这一列最容易出、也最难查的一个错。
//  3. 脏数据要当场报错，不能静默读成空 —— 静默读成空等于把偏好悄悄抹掉。
func TestCityCodesRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		codes CityCodes
		// 写出去的字面量；空为 nil（NULL）
		literal any
	}{
		{"一个城市", CityCodes{310000}, "{310000}"},
		{"五个城市（上限）", CityCodes{310000, 330100, 440300, 110000, 510100}, "{310000,330100,440300,110000,510100}"},
		{"空列表写成 NULL，不是 '{}'", CityCodes{}, nil},
		{"nil 同样是 NULL", nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.codes.Value()
			if err != nil {
				t.Fatalf("Value() 报错: %v", err)
			}
			if v != tc.literal {
				t.Fatalf("Value() = %#v，期望 %#v", v, tc.literal)
			}

			// 再读回来。驱动交回来的形态是字符串（也可能被包成 []byte）
			var back CityCodes
			if err := back.Scan(v); err != nil {
				t.Fatalf("Scan(%#v) 报错: %v", v, err)
			}
			if len(back) != len(tc.codes) {
				t.Fatalf("读回来 %v，期望 %v", back, tc.codes)
			}
			for i := range tc.codes {
				if back[i] != tc.codes[i] {
					t.Fatalf("读回来 %v，期望 %v", back, tc.codes)
				}
			}
		})
	}
}

func TestCityCodesScanVariants(t *testing.T) {
	cases := []struct {
		name string
		src  any
		want CityCodes
	}{
		{"NULL", nil, nil},
		{"空数组读成空，与 NULL 同义", "{}", nil},
		{"空串也是空", "", nil},
		{"字节切片", []byte("{310000,330100}"), CityCodes{310000, 330100}},
		{"带空格也认", "{ 310000 , 330100 }", CityCodes{310000, 330100}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got CityCodes
			if err := got.Scan(tc.src); err != nil {
				t.Fatalf("Scan(%#v) 报错: %v", tc.src, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Scan(%#v) = %v，期望 %v", tc.src, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("Scan(%#v) = %v，期望 %v", tc.src, got, tc.want)
				}
			}
		})
	}
}

// 读不懂的东西必须报错。读成空的话，这个人的「期望城市」会被
// 悄悄改成「不限」，而界面上看不出任何变化。
func TestCityCodesScanRejectsGarbage(t *testing.T) {
	for _, src := range []any{"310000", "{310000", "310000}", "{上海}", "{310000,}", 12345} {
		var c CityCodes
		if err := c.Scan(src); err == nil {
			t.Fatalf("Scan(%#v) 没有报错，读成了 %v", src, c)
		}
	}
}
