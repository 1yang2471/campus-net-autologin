package keeper

import "testing"

// 流量能力升级 · 切片1：把门户返回的「带单位字符串」解析成字节数。
//
// 真机语料（2026-11-11 / keeper.log 历史）：当天流量 2.40006G、603.281M、
// 日流量额 4G / 6G；异校语料还有 "1.2 GB"。字段名是校事实，但**量纲写法**
// 可归一。解析不出来必须明确返回 false——绝不能猜个数出来算百分比。

func TestParseFluxBytes(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"2.40006G", 2.40006 * 1024 * 1024 * 1024},
		{"603.281M", 603.281 * 1024 * 1024},
		{"4G", 4 * 1024 * 1024 * 1024},
		{"6G", 6 * 1024 * 1024 * 1024},
		{"1.2 GB", 1.2 * 1024 * 1024 * 1024},
		{"512K", 512 * 1024},
		{"0G", 0},
		{"  3.5G  ", 3.5 * 1024 * 1024 * 1024},
		{"7T", 7 * 1024 * 1024 * 1024 * 1024},
	}
	for _, c := range cases {
		got, ok := ParseFluxBytes(c.in)
		if !ok {
			t.Errorf("ParseFluxBytes(%q) 应能解析", c.in)
			continue
		}
		if diff := got - c.want; diff > 1 || diff < -1 {
			t.Errorf("ParseFluxBytes(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}
}

// 行为：不认识的写法/空值/纯数字无单位 → 算不出，不硬猜。
func TestParseFluxBytesUnparseable(t *testing.T) {
	for _, in := range []string{"", "未知", "N/A", "abc", "-", "12", "G", "1.2XB", "1,2G"} {
		if got, ok := ParseFluxBytes(in); ok {
			t.Errorf("ParseFluxBytes(%q) 应判算不出，实际 %v", in, got)
		}
	}
}
