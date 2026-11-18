package main

import (
	"fmt"

	"cnal/internal/keeper"
)

// fluxWarnMessage 把内核的一次性越阈事件翻成人话。跨平台纯函数：
// Windows 托盘气泡与安卓前台服务通知共用同一份文案，不各自编码。
// 克制、具体、不喊——标题短到通知栏不截断，正文只报读数与阈值。
func fluxWarnMessage(level string, v keeper.FluxUsageView) (title, body string) {
	switch level {
	case "critical":
		title = "校园网流量将用尽"
	default:
		title = "校园网流量提醒"
	}
	if v.HasPercent {
		pct := int(v.Percent + 0.5)
		if v.Quota != "" {
			return title, fmt.Sprintf("今日已用 %s / %s，约 %d%%", v.Used, v.Quota, pct)
		}
		return title, fmt.Sprintf("今日已用 %s，约 %d%%", v.Used, pct)
	}
	// 算不出百分比：原文照列，不编造数字。
	if v.Used != "" && v.Quota != "" {
		return title, fmt.Sprintf("今日已用 %s / %s", v.Used, v.Quota)
	}
	if v.Used != "" {
		return title, "今日已用 " + v.Used
	}
	return title, "已查询到流量信息，请在面板查看"
}
