package main

import "cnal/internal/api"

// tooltipLine 托盘 tooltip 全量拼法（2026-11-18 双读数）：状态 + 详情 + 用户
// 路径。用户路径只在 broken 时加一句——ok 不占长度，未知也不猜。
// 2026-11-22 修复项 4：菜单行与 tooltip 分家——菜单行吃内核 MenuLine 短形态
// （括号补充说明不进菜单行），tooltip 保留完整读数（排查第一现场）。
func tooltipLine(s api.Snapshot) string {
	line := s.Label
	if s.Detail != "" {
		line += "｜" + s.Detail
	}
	if s.UserPath == "broken" {
		line += "｜用户路径不通"
	}
	return line
}

// statusLine 托盘菜单行：内核 MenuLine 短形态优先（单源渲染——截断规则在
// 内核 api.MenuLine 一处定稿），未生成时兜底走全量拼法（测试替身/旧快照）。
func statusLine(s api.Snapshot) string {
	if s.MenuLine != "" {
		return s.MenuLine
	}
	return tooltipLine(s)
}
