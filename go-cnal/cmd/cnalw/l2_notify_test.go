package main

import (
	"strings"
	"testing"
)

// 风控窗口气泡文案（2026-11-13 黑盒一小时的产品解：进入 DPI 风控窗口时
// 用户该被「看见」，而不是醒来发现网坏了不知道为什么）。文案是纯函数可测；
// 真弹气泡（Shell_NotifyIcon）是平台调用，不进单测——与 flux_notify 同纪律。

// 行为：文案点齐三件事——发生了什么（出口受限）、要不要动手（勿反复重登）、
// 认证是否正常（认证正常）。不得把未验证的推断写成事实（2026-11-18 修正：
// 「DPI 风控窗口」「约 15 分钟自愈」是推断，日志只证明大众站 TLS 被重置）。
func TestL2BalloonMessage(t *testing.T) {
	title, body := l2BalloonMessage()
	if title == "" || body == "" {
		t.Fatal("标题/正文不能空")
	}
	for _, w := range []string{"认证正常", "勿反复重登"} {
		if !strings.Contains(title+body, w) {
			t.Errorf("文案缺 %q：title=%q body=%q", w, title, body)
		}
	}
	for _, bad := range []string{"15 分钟", "DPI"} {
		if strings.Contains(title+body, bad) {
			t.Errorf("不得把未验证的推断写成事实（%q）：%q %q", bad, title, body)
		}
	}
	if strings.Contains(title+body, "!") || strings.Contains(title+body, "！") {
		t.Errorf("文案不得带感叹号：%q %q", title, body)
	}
}

// 行为：文案长度有界——气泡标题栏 64 wchar、正文 256 wchar 硬上限，
// 超长会被截断，超长就是设计缺陷。
func TestL2BalloonMessageLength(t *testing.T) {
	title, body := l2BalloonMessage()
	if len([]rune(title)) > 64 || len([]rune(body)) > 256 {
		t.Errorf("文案过长 title=%d body=%d", len([]rune(title)), len([]rune(body)))
	}
}
