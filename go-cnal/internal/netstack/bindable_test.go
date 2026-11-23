package netstack

import (
	"net"
	"testing"
	"time"
)

// 2026-11-23 过夜缺陷的真实自检测试（不靠替身）：网卡 IP 被 Doze 摘掉后
// LocalAddr 绑不上（cannot assign requested address）——Bindable 必须判 false。
// 用文档保留地址 203.0.113.x（不会是本机的真实源）模拟「已失效 IP」。
func TestBindableRejectsUnassignableSource(t *testing.T) {
	c := New(net.ParseIP("203.0.113.99"), time.Second)
	if c.Bindable(nil) {
		t.Error("已失效/不存在于本机的源 IP 必须判不可绑定")
	}
}

// 回环地址一定可绑定（每机都有）——守「正常源不误判」，避免自检把好源
// 也判死导致全部流量被迫转系统栈（凭据泄露到 TUN 的风险）。
func TestBindableAcceptsLoopback(t *testing.T) {
	c := New(net.ParseIP("127.0.0.1"), time.Second)
	if !c.Bindable(nil) {
		t.Error("回环源应可绑定（自检不得误判正常源）")
	}
}

// 系统栈无源可绑：自检必须跳过（由系统路由决定，不是物理栈纪律范畴）。
func TestBindableSystemAlwaysTrue(t *testing.T) {
	c := NewSystem(time.Second)
	if !c.Bindable(net.ParseIP("203.0.113.99")) {
		t.Error("系统栈无绑源概念，Bindable 应恒 true")
	}
}

// 显式 IP 参数覆盖 client 自身源（chooseRoute 传 bound.Source() 之外的场景，
// 如自检某个候选源而不改 client 状态）。
func TestBindableExplicitArgOverridesClientSource(t *testing.T) {
	c := New(net.ParseIP("203.0.113.99"), time.Second) // client 自身源无效
	if !c.Bindable(net.ParseIP("127.0.0.1")) {          // 显式给有效源
		t.Error("显式 IP 参数应覆盖 client 自身源")
	}
}
