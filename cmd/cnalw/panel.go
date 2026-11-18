package main

import (
	"embed"
	"strings"
)

// 面板源料拆四份（HTML 骨架 / CSS / 状态侧 JS / 设置侧 JS），提交钩子按 20KB
// 上限逼出来的结构；对外仍是同一份自包含页面——/panel 只返回一个 HTML 文档。
//
//go:embed panel.html panel.css panel.js panel_settings.js
var panelFS embed.FS

// panelJS 返回面板脚本：状态侧 + 设置侧按序拼接。拼接顺序即执行顺序，
// 两侧共用同一顶层作用域，拆开只为过 20KB 上限，不是模块边界——所以
// /panel 与测试都走这一个入口（页面拿到的脚本与断言的对象永远是同一份）。
func panelJS() string {
	status, _ := panelFS.ReadFile("panel.js")
	settings, _ := panelFS.ReadFile("panel_settings.js")
	return string(status) + "\n" + string(settings)
}

// panelPage 把四份源料拼回完整面板页（占位符各替一次；Replace 不解释 $）。
func panelPage() string {
	html, _ := panelFS.ReadFile("panel.html")
	css, _ := panelFS.ReadFile("panel.css")
	page := strings.Replace(string(html), "{{PANEL_CSS}}", string(css), 1)
	return strings.Replace(page, "{{PANEL_JS}}", panelJS(), 1)
}
