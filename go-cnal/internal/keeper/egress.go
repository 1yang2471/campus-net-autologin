package keeper

import (
	"errors"
	"fmt"
)

// 服务出口健康（2026-11-18 多源抖动实证）。
//
// 为什么单独一条缝：绑定源可能在选中后几十秒才真正失效（日志 17:47:39 选中
// 10.141.53.34，17:48:19 绑源 DNS 超时）。这时 L1 红，而门户/登录会回退系统
// 栈却绿——若把「本地绑定失效」当成「会话丢失」，就会用另一条路登录，自己的
// 探针永远不绿，烧预算并触发自伤熔断。出口健康是登录的前置门槛：不健康就
// 不发凭据，先重选。
type EgressHealth struct {
	// Valid=false 表示当前服务出口不能承载认证（绑定失效/门户不可达）。
	// 零值即不健康；组合根健康时必须显式给 Valid=true。
	Valid bool
	// Reason 失败分类（展示与重选策略用）。
	Reason EgressReason
	// Detail 人话原因（状态行/日志；不得写猜测，没证据就留空）。
	Detail string
	// Source 当前绑定的源地址（展示用；空=未知）。
	Source string
}

// EgressReason 出口失败分类（唯一出处；壳层不得自造词表）。
type EgressReason string

const (
	EgressOK EgressReason = ""
	// EgressBindingInvalid 绑定的本地源地址已失效（bind: address not valid）。
	EgressBindingInvalid EgressReason = "binding_invalid"
	// EgressPortalUnreachable 绑定源到不了门户（绑源解析/连接失败）。
	EgressPortalUnreachable EgressReason = "portal_unreachable"
)

// ErrEgressInvalid 自动登录前的出口门槛哨兵（app 层登录缝的二次防线）。
var ErrEgressInvalid = errors.New("服务出口不可用")

// ErrEgressPortalUnreachable 绑源到不了门户的哨兵：与 ErrEgressInvalid 分开，
// 因为前者只该重选、不该把当前源标降级（一次 DNS 抖动不等于网卡死了）。
var ErrEgressPortalUnreachable = errors.New("服务出口到不了门户")

// egress 读当前出口健康；未接线按健康处理（旧装配/旧测试零影响）。
func (d *Daemon) egress() EgressHealth {
	if d.EgressHealth == nil {
		return EgressHealth{Valid: true}
	}
	return d.EgressHealth()
}

// egressDetail 出口失效的状态行（单一出处）：必须说清是哪条出口、为什么、
// 正在做什么。没证据的部分不写。
func egressDetail(eg EgressHealth) string {
	why := eg.Detail
	if why == "" {
		why = "原因不可得"
	}
	if eg.Source != "" {
		return fmt.Sprintf("服务出口 %s 不可用（%s），正在重选", eg.Source, why)
	}
	return fmt.Sprintf("服务出口不可用（%s），正在重选", why)
}
