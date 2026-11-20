package keeper

import "time"

// 通道级自愈登录（2026-11-20 双通道方案 §8.3 M2）。
//
// **账号级预算共享**的实现方式：不新建闸门。通道登录动作收口在本文件，
// 直接复用 Daemon 主链路那一份 lastLogin（30s 最小间隔）与 app 层 RateLimit
// （8 次/小时）——于是「两条通道同时掉线时登录频率仍受同一套闸门约束」是
// 结构上成立的，而不是靠两边自觉同步。
//
// 门户的反暴力冻结是**账号级**的（实测 12 次/小时触发）。按通道各给一份
// 额度等于把冻结线翻倍——那正是 2026-11-19「旁路只观察不登录」纪律要防的。
// 本文件正面解决那条顾虑：共享预算 + 默认关（见 SetMultiChannel）。
//
// 动作前置条件（任一不满足就不发凭据）：
//   - 通道状态 = unauth（门户劫持）。黑洞/离线态登录请求同样发不出去，
//     白撞门户还烧预算——不登录。
//   - 账号级闸门放行（30s 间隔 / 通道退避 / 通道冻结 / 速率上限）。
//   - auto_reconnect 开着（关掉 = 只探测报状态，不自动登录）。
//   - 不在免认证时段/免会话态（那两个态下门户不受理登录）。

// channelLoginInterval 通道登录的最小尝试间隔（与主链路共用 30s 闸门；
// 这里额外要求：同一通道两次登录之间至少这么久——避免一条抖动通道
// 反复触发登录把预算烧光）。
const channelLoginInterval = 60 * time.Second

// ChannelLoginFn 通道登录缝：对指定通道发起一次登录（绑该通道的源 IP）。
// 返回门户响应原文（判「已在线」用）。nil = 未接线（不动作）。
type ChannelLoginFn func(ch ChannelView) (string, error)

// tryChannelLogin 对未认证通道发起一次登录。返回是否真的登录了。
//
// 只处理**一条**通道：一次 tick 最多发一次通道登录（多通道同时掉线时
// 下一拍再处理下一条）——这与主链路的 30s 闸门共同保证请求量有界。
func (d *Daemon) tryChannelLogin(now time.Time) bool {
	if !d.multiChannel.Load() {
		return false
	}
	if d.ChannelLogin == nil {
		return false
	}
	// 与主链路同一序：开关 → 免认证 → 退避/冻结 → 30s 间隔。
	if !d.autoReconnect.Load() {
		return false
	}
	if d.inFreeWindow() {
		return false
	}
	if now.Before(d.channelRejectDeadline()) || now.Before(d.channelFrozenDeadline()) {
		return false
	}
	if d.lastLogin.Add(loginMinInterval).After(now) {
		return false // 账号级 30s 闸：主链路刚登录过就让路
	}
	// 免会话态（锚点绿但门户不受理登录）时同样不自动登录。
	if _, _, _, _, freeMode := d.View(); freeMode {
		return false
	}
	views := d.ChannelViews()
	for _, v := range views {
		if v.State != ChannelUnauth {
			continue
		}
		if d.channelLastAttempt(v.ID).Add(channelLoginInterval).After(now) {
			continue // 该通道刚试过：等一会儿再说
		}
		d.lastLogin = now // 账号级闸门：占住 30s 间隔
		d.markChannelAttempt(v.ID, now)
		resp, err := d.ChannelLogin(v)
		if err != nil {
			d.channelLoginFailed(now, err)
			return true
		}
		// 「已在线」也算恢复（门户可能这样答——它认的是这条通道的源 IP，
		// 答「已在线」说明会话其实在）。与主链路 loginSucceeded 同款判定。
		d.clearChannelAttempt(v.ID)
		_ = resp
		return true
	}
	return false
}

// channelLoginFailed 通道登录失败：进**账号级通道退避**。
//
// 为什么不能直接用主链路的 rejectUntil：主链路的「会话仍活」分支会清掉
// rejectUntil（daemon_probe.go 在线分支）——那对主链路是对的（会话还活着，
// 上次拒绝已不成问题），但会连带抹掉通道的退避：通道登录在同一拍被拒 →
// 退避刚设上就被主链路抹掉 → 下一拍继续撞门户 = 白烧预算。
// 2026-11-20 实现时踩到过这个坑，故分账：通道退避独立，主链路无权清它。
//
// 但它仍是**账号级**的：门户拒的是账号，任一通道被拒，所有通道一起退避。
func (d *Daemon) channelLoginFailed(now time.Time, err error) {
	wait := d.classifyRejection(err)
	d.mu.Lock()
	d.channelReject = now.Add(wait)
	d.mu.Unlock()
	// 冻结单独成闸（与主链路同款）：普通退避会被会话复活清零，冻结闸不会。
	if frozen, fwait := freezeWait(err.Error(), d.marks, d.rejectBackoff); frozen {
		d.mu.Lock()
		d.channelFrozen = now.Add(fwait)
		d.mu.Unlock()
	}
}

// channelRejectUntil 通道退避截止（账号级；与主链路分账，见 channelLoginFailed）。
func (d *Daemon) channelRejectDeadline() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.channelReject
}

// channelFrozenUntil 通道冻结截止（账号级）。
func (d *Daemon) channelFrozenDeadline() time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.channelFrozen
}

// channelLastAttempt 某通道最近一次登录尝试时刻（零值 = 没试过）。
func (d *Daemon) channelLastAttempt(id string) time.Time {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.channelLastLogin[id]
}

// markChannelAttempt 记某通道刚尝试过（用于「刚试过」的间隔判定）。
func (d *Daemon) markChannelAttempt(id string, at time.Time) {
	d.mu.Lock()
	if d.channelLastLogin == nil {
		d.channelLastLogin = map[string]time.Time{}
	}
	d.channelLastLogin[id] = at
	d.mu.Unlock()
}

// clearChannelAttempt 清掉某通道的尝试间隔标记（登录成功后调用）。
func (d *Daemon) clearChannelAttempt(id string) {
	d.mu.Lock()
	delete(d.channelLastLogin, id)
	d.mu.Unlock()
}
