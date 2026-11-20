package keeper

import (
	"errors"
	"time"
)

// 登录动作与失败分类（从 daemon.go 拆出——单文件守 2 万字节提交闸，tidy §3）。
// 语义仍是 Tick 的重连段：成功点亮在线并清判死；失败按类别进退避/熔断。
// 调用方 Tick 负责前置闸门（退避/节流/免认证时段），本文件只管「发一次登录
// 之后怎么记账」。

// doLogin 执行一次登录并处理全部结果分支。l1 为本次拍的锚点结论（免会话态
// 判定要用）。返回前必定 setState，调用方无需再处理。
func (d *Daemon) doLogin(now time.Time, l1 bool) {
	d.lastLogin = now
	_, _, _, wasDead, _ := d.View()
	resp, err := d.Login()
	if err == nil {
		d.loginSucceeded(now, resp, wasDead)
		return
	}
	d.loginFailed(now, err, l1)
}

// loginSucceeded 登录成功分支：清判死/免会话态，跑 SESSION_FALSE 与熔断计数。
func (d *Daemon) loginSucceeded(now time.Time, resp string, wasDead bool) {
	d.setSessionDead(false)
	d.setFreeMode(false)
	d.freeStreak = 0
	d.invalidateSessionCache() // 登录后会话已变：下次 L1 红须重新问门户
	// SESSION_FALSE 自停闸：判死→重登→门户答「已在线」= flux 在撒谎，
	// 矛盾 ×3 → 探针自停 3600s（假判死不得变成登录风暴）。
	if wasDead && alreadyOnline(resp) {
		d.sessFalse++
		if d.sessFalse >= sessFalseLimit {
			d.fluxOffUntil = now.Add(sessFalseDisable)
			d.sessFalse = 0
		}
	}
	d.suspect++
	if d.suspect >= suspectLimit {
		d.rejectUntil = now.Add(d.rejectBackoff)
		d.suspect = 0
		d.setState(StateRejected, "连续 3 次登录成功但探测仍失败——疑似误判，熔断退避")
		return
	}
	// 判定链健康：清退避与瞬态连击（Python 成功分支 reject_until=0、
	// transient_streak=0 同源）。
	d.rejectUntil = time.Time{}
	d.transientStreak = 0
	// 主链路登录成功 = 门户重新接受本账号 → 通道级退避一并解除
	// （2026-11-20 双通道：通道退避记录的是「账号被拒」，主登录成功即证明
	// 它已成陈账；不清掉通道会在退避期内拒绝自愈）。
	d.mu.Lock()
	d.channelReject = time.Time{}
	d.channelFrozen = time.Time{}
	d.mu.Unlock()
	d.setState(StateOnline, "")
}

// loginFailed 登录失败分支：先认出口类错误（这次登录没发出，不算尝试），
// 再按冻结/速率/瞬态/硬错误分级退避。
func (d *Daemon) loginFailed(now time.Time, err error, l1 bool) {
	// 出口在门槛放行后失效：这次登录没有发出，不能算一次尝试——
	// 清掉 lastLogin、不进退避/熔断，报状态并请组合根重选。
	if errors.Is(err, ErrEgressInvalid) {
		d.lastLogin = time.Time{}
		d.setState(StateOffline, err.Error())
		if d.OnEgressInvalid != nil {
			d.OnEgressInvalid()
		}
		return
	}
	if errors.Is(err, ErrEgressPortalUnreachable) {
		d.lastLogin = time.Time{}
		d.setState(StatePortalDown, err.Error())
		if d.OnPortalDown != nil {
			d.OnPortalDown()
		}
		return
	}
	// 门户限频冻结：独立闸门（Python _frozen_until 同源）——普通退避
	// 会被 flux 复活清零，冻结闸不会，否则「抖动→放行→再撞→更深冻结」。
	if frozen, wait := freezeWait(err.Error(), d.marks, d.rejectBackoff); frozen {
		d.frozenUntil = now.Add(wait)
		d.rejectUntil = now.Add(wait)
		d.setState(StateRejected, err.Error())
		return
	}
	// 速率闸门（app 层哨兵）：Python 显示 offline 而非 rejected。
	if errors.Is(err, ErrRateCapped) {
		d.rejectUntil = now.Add(d.transientBackoff)
		d.setState(StateOffline, err.Error())
		return
	}
	// 拒绝分类退避（Python handle_rejection 同源）：瞬态错误短退避快速
	// 重试（防明早时段切换卡 10 分钟），连续 5 次升级长退避；硬错误
	// （密码/额度/锁定）直接长退避。
	wait := d.classifyRejection(err)
	d.rejectUntil = now.Add(wait)
	if l1 {
		d.freeStreak++
		if d.freeStreak >= freeDetectStreak {
			d.setFreeMode(true)
			d.setState(StateFreeWindow, "免认证时段（自动检测）：公网可用、门户不受理登录")
			return
		}
	}
	d.setState(StateRejected, err.Error())
}

// classifyRejection 拒绝分类：瞬态错误短退避（连续 5 次升级长退避），
// 硬错误（密码/额度/锁定）直接长退避。主链路与通道登录**共用这一套**
// ——门户拒的是账号，两条路径对「这次拒绝该退多久」的判断必须一致。
func (d *Daemon) classifyRejection(err error) time.Duration {
	if d.marks.transientMarked(err.Error()) {
		d.transientStreak++
		if d.transientStreak < transientStreakLimit {
			return d.transientBackoff
		}
		return d.rejectBackoff
	}
	d.transientStreak = 0
	return d.rejectBackoff
}