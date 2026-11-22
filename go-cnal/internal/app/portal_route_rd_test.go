package app

import (
	"errors"
	"net"
	"testing"
	"time"
)

// 2026-11-22 复盘内因 3：rd 触发面拓宽——chooseRoute 转系统栈时触发物理源
// 重发现（rd.tick）。11-22 事故：插线恢复（绑源 IP 复活）后，绑源解析恢复
// 前门户路径一直赖在系统栈 10 分钟（pt 缓存 + rd 触发面窄的复合）。
// 复盘要求「先写测试钉行为」：转系统栈 → rd 被调；绑源路径 → 不被调。

// 行为：auto 转系统栈时触发 rd.tick（rd 的 interval 节流保证热路径安全——
// PortalAlive 每拍都选路）。
func TestRouteAutoSystemFallbackTriggersRdTick(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return nil, errors.New("DNS 不通")
	})
	ticks := 0
	p.rd = newRediscoverer(0, net.IPv4(10, 141, 53, 34),
		func() net.IP {
			ticks++
			return nil // 发现失败保持原源（fail-closed，不计入换绑）
		},
		func(net.IP) {})
	p.routePortal()
	if ticks != 1 {
		t.Errorf("转系统栈应触发 rd.tick，实际 %d 次", ticks)
	}
	p.routePortal()
	if ticks != 2 {
		t.Errorf("interval=0 时每次转系统栈都应 tick，实际 %d 次", ticks)
	}
}

// 行为：绑源解析恢复回 bound 路径后不再触发 rd（重发现是源失效场景的职权）。
func TestRouteBoundPathDoesNotTriggerRdTick(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	ticks := 0
	p.rd = newRediscoverer(time.Hour, net.IPv4(10, 141, 53, 34),
		func() net.IP { ticks++; return nil }, func(net.IP) {})
	p.routePortal()
	if ticks != 0 {
		t.Errorf("绑源路径不得触发 rd.tick，实际 %d 次", ticks)
	}
}
