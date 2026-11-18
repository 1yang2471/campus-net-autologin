//go:build !windows

package main

// 非 Windows 构建：cnalw 是 Windows 托盘程序，但同包测试/交叉编译要能过。
// 气泡是平台通道，这里空实现；文案与事件语义仍由 fluxWarnMessage / keeper 保证。
func showBalloon(title, body string, critical bool) {}
