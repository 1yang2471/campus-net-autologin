package keeper

import "time"

// 守护动作时间线（2026-11-21 产品优化决议 C）：面板状态卡显示最近一次
// 守护动作「N 分钟前·自动登录」——断网排查时用户第一眼知道守护动没动过，
// 不用翻日志反推（当日复盘：通道自愈在状态日志里无痕，用户只能靠 /state
// 反推）。写入点：主链路登录成功（daemon_login.go）、通道登录成功
//（channel_login.go）、切账号成功（app 层 compose_api.go）。

// RecordAction 记一次守护动作（加锁写：写入点含 API goroutine 的切账号）。
// kind 用人话（「自动登录」「通道登录(WLAN 2)」「切换账号」），面板原样显示。
func (d *Daemon) RecordAction(kind string) {
	at := d.Now()
	d.mu.Lock()
	d.lastAction.Kind = kind
	d.lastAction.At = at
	d.mu.Unlock()
}

// LastAction 最近一次守护动作读数（加锁读，与 RecordAction 配对）。
// kind 为空 = 进程内还没动作过——面板显示「尚未动作」。
func (d *Daemon) LastAction() (kind string, at time.Time) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastAction.Kind, d.lastAction.At
}
