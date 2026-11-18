package main

import "cnal/internal/api"

// statusLine 托盘 tooltip / 菜单状态行的统一拼法（2026-11-18 双读数）：
// 状态 + 详情 + 用户路径。用户路径只在 broken 时加一句——ok 不占长度，
// 未知也不猜。
func statusLine(s api.Snapshot) string {
	line := s.Label
	if s.Detail != "" {
		line += "｜" + s.Detail
	}
	if s.UserPath == "broken" {
		line += "｜用户路径不通"
	}
	return line
}
