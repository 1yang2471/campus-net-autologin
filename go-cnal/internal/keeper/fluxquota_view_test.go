package keeper

import "testing"

// 切片2：用量快照。字段名是校事实（当天流量/日流量额 vs 已用流量/剩余流量），
// 按既有优先级取；能解析就给百分比，算不出就把原文照列、百分比留空。

func TestFluxUsageFromCampusData(t *testing.T) {
	v := FluxUsage(map[string]string{
		"用户名称": "23ykyang",
		"当天流量": "2.40006G",
		"日流量额": "4G",
		"帐号状态": "正常",
	})
	if v.Used != "2.40006G" || v.Quota != "4G" {
		t.Fatalf("原文应照列，实际 used=%q quota=%q", v.Used, v.Quota)
	}
	if !v.HasPercent || v.Percent < 59 || v.Percent > 61 {
		t.Errorf("2.40006G/4G 应约 60%%，实际 %.1f has=%v", v.Percent, v.HasPercent)
	}
	if v.Level != "ok" {
		t.Errorf("60%% 应是 ok，实际 %q", v.Level)
	}
}

// 行为：异校键名（已用流量/剩余流量）也要吃；绝不把「用户名称」当流量。
func TestFluxUsageAlternateKeys(t *testing.T) {
	v := FluxUsage(map[string]string{
		"用户名称": "23jszeng",
		"已用流量": "1.2 GB",
		"剩余流量": "2.8 GB",
	})
	// 剩余流量不是额度：没有额度键时百分比算不出，但已用原文要在。
	if v.Used != "1.2 GB" {
		t.Errorf("已用流量原文应保留，实际 %q", v.Used)
	}
	if v.HasPercent {
		t.Errorf("只有「剩余流量」没有额度，不该硬算百分比，实际 %.1f", v.Percent)
	}
}

// 行为：缺字段/单位不认识 → 百分比空、原文保留，不报错不造假。
func TestFluxUsageUnparseableKeepsRaw(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]string
	}{
		{"缺额度", map[string]string{"当天流量": "603.281M"}},
		{"单位不认识", map[string]string{"当天流量": "若干", "日流量额": "很多"}},
		{"空表", map[string]string{}},
	} {
		v := FluxUsage(tc.data)
		if v.HasPercent {
			t.Errorf("%s：不该算出百分比，实际 %.1f", tc.name, v.Percent)
		}
		if len(v.Raw) == 0 && len(tc.data) > 0 {
			t.Errorf("%s：原文应照列", tc.name)
		}
	}
}

// 行为：百分比上限 100（已用超过额度时按 100 显示，不显示 130%）。
func TestFluxUsagePercentClamped(t *testing.T) {
	v := FluxUsage(map[string]string{"当天流量": "5G", "日流量额": "4G"})
	if !v.HasPercent || v.Percent != 100 {
		t.Errorf("超额度应夹到 100，实际 %.1f has=%v", v.Percent, v.HasPercent)
	}
}
