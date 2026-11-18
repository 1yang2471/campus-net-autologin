package main

import (
	"fmt"
	"os"

	"cnal/internal/app"
)

// runSetup 首次运行配置：自动探测门户 → 打开面板填账号密码 → 返回是否已完成。
// 返回 false 时调用方以 0 退出（引导已给，不是错误）；用户下次启动可继续。
// 面板保存即写 ini + Windows 凭据管理器，窗口关闭后重新 Compose 走正常启动。
func runSetup(cfgPath string) bool {
	showMessage("首次运行：未找到可用配置。\n点击「确定」后自动探测校园门户，随后打开设置面板。",
		"校园网守护 · 首次配置")
	s, err := app.NewSetup(cfgPath)
	if err != nil {
		showMessage("首次配置初始化失败：\n"+err.Error()+
			"\n\n也可运行 cnal detect --write 生成配置后重试。", "校园网守护")
		return false
	}
	if _, err := s.API.Listen(); err != nil {
		showMessage("首次配置面板启动失败：\n"+err.Error(), "校园网守护")
		return false
	}
	s.API.PanelPage = panelPage()
	url := s.API.PanelURL() + "&setup=1" // 隐藏运行期动作/诊断卡（端点未接线）
	msg := "请在即将打开的面板中填写账号与密码并保存。\n密码只存 Windows 凭据管理器，不写配置文件。"
	if s.Detected != "" {
		msg += "\n\n已自动探测到门户：" + s.Detected
	} else {
		msg += "\n\n未自动探测到门户（可能已登录）：请在面板中手工填写门户地址。"
	}
	showMessage(msg, "校园网守护 · 首次配置")
	if err := openPanelWindow(url); err != nil {
		fmt.Fprintln(os.Stderr, "原生面板不可用，回落浏览器:", err)
		_ = openURL(url)
		showMessage("配置面板已在浏览器打开。\n保存后请重新启动校园网守护。", "校园网守护")
		return false
	}
	if !s.Ready() {
		showMessage("未完成账号/密码或门户配置，本次不启动守护。\n"+
			"重新打开程序可继续配置，或运行 cnal detect --write 生成配置。", "校园网守护")
		return false
	}
	return true
}
