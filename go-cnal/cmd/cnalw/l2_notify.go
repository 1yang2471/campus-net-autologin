package main

// 风控窗口气泡文案：进入 DPI 风控窗口时点齐三件事——发生了什么、要不要
// 动手、什么时候会好。跨平台纯函数（Windows 托盘气泡用；安卓 FGS 通知靠
// detail 天然携带同款信息，不各编一份）。
func l2BalloonMessage() (title, body string) {
	title = "校园网出口受限"
	body = "校园侧风控窗口：部分网站可能暂时打不开，认证正常，约 15 分钟自愈，无需操作。"
	return title, body
}
