// 物理源失效自愈的 AAR 导出（2026-11-24）：注入 v4 是启动快照，换 WiFi 后
// 只写全局变量不够——需显式重绑已 Compose 的客户端；绑源失效时反向通知
// Kotlin 重新取 LinkProperties。两条缝都只在注入模式生效，Windows 零调用。
package mobile

import "cnal/internal/app"

// SourceSink 物理源失效通知缝（Kotlin 实现，gomobile 绑定成 Java 接口）。
// 绑定失效时 Go 只发信号，真正的 LinkProperties 重取在 Kotlin（D-09）。
type SourceSink interface {
	SourceStale()
}

// SetSourceSink 注入 SourceStale 消费者（Kotlin 侧实现生成的 Java 接口）。
// nil = 不通知（桌面内核不用）。
func SetSourceSink(s SourceSink) {
	if s == nil {
		app.SetSourceStaleSink(nil)
		return
	}
	app.SetSourceStaleSink(s.SourceStale)
}

// RebindSources 换 WiFi / 路由器重启后重绑已注入的物理源（AAR 导出）。
// Kotlin 先 SetPhysicalSources 写新值，再调本函数让四个绑源客户端真正换绑
// （注入 v4 是启动快照，只写全局不改绑——2026-11-24 根因）。
// 非注入模式/尚未启动时 no-op。
func RebindSources() error { return app.RebindInjectedSources() }
