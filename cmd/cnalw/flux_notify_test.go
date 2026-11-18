package main

import (
	"strings"
	"testing"

	"cnal/internal/keeper"
)

// 通知缝：内核给一次性越阈事件，壳层把它变成人话。文案是纯函数，可测；
// 真正的弹通知（Windows 气泡 / 安卓前台服务）是平台调用，不进单测。
// 需求：短、具体、不喊、不带感叹号、不夸大。

func TestFluxWarnMessage(t *testing.T) {
	cases := []struct {
		level string
		view  keeper.FluxUsageView
		want  []string
	}{
		{"warn", keeper.FluxUsageView{Used: "3.2G", Quota: "4G", Percent: 80, HasPercent: true},
			[]string{"80", "3.2G", "4G"}},
		{"critical", keeper.FluxUsageView{Used: "3.7G", Quota: "4G", Percent: 92, HasPercent: true},
			[]string{"92", "3.7G", "4G"}},
	}
	for _, c := range cases {
		title, body := fluxWarnMessage(c.level, c.view)
		if title == "" || body == "" {
			t.Fatalf("%s：标题/正文不能空", c.level)
		}
		for _, w := range c.want {
			if !strings.Contains(title+body, w) {
				t.Errorf("%s 文案缺 %q：title=%q body=%q", c.level, w, title, body)
			}
		}
		if strings.Contains(title+body, "!") || strings.Contains(title+body, "！") {
			t.Errorf("%s 文案不得带感叹号：%q %q", c.level, title, body)
		}
	}
}

// 行为：算不出百分比时仍给可用文案（原文照列），不编造数字。
func TestFluxWarnMessageWithoutPercent(t *testing.T) {
	title, body := fluxWarnMessage("warn", keeper.FluxUsageView{
		Used: "3.2G", Quota: "4G", HasPercent: false,
		Raw: map[string]string{"当天流量": "3.2G", "日流量额": "4G"},
	})
	if title == "" || body == "" {
		t.Fatal("无百分比时也该有文案")
	}
	if !strings.Contains(title+body, "3.2G") {
		t.Errorf("原文应保留：%q %q", title, body)
	}
}

// 行为：文案长度有界——托盘气泡/通知栏会被截断，超长就是设计缺陷。
func TestFluxWarnMessageLength(t *testing.T) {
	title, body := fluxWarnMessage("critical", keeper.FluxUsageView{
		Used: "3.7G", Quota: "4G", Percent: 92, HasPercent: true,
	})
	if len([]rune(title)) > 24 || len([]rune(body)) > 64 {
		t.Errorf("文案过长 title=%d body=%d", len([]rune(title)), len([]rune(body)))
	}
}
