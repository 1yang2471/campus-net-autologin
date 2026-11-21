package keeper

// 从 daemon.go 拆出（2026-11-21，守 2 万字节提交闸——tidy §3 同款纪律）：
// 守护的**对外交接面**——展示字段读写、多通道读数转发与运行开关。
// 语义不变：setState/View 的锁纪律见各方法注（规则表「展示字段无数据竞争」）。
// 动作时间线（决议 C）住在 daemon_action.go。

// setState 状态行写入（Tick goroutine 调用；与 View/StateForUI 的加锁读配对）。
func (d *Daemon) setState(state, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.State = state
	d.Detail = detail
}

// View 展示字段快照（回路 API / 托盘用）：加锁读，与 Tick 的 setState 写配对。
// 值拷贝返回，调用方拿到的不再是共享内存。
func (d *Daemon) View() (state, detail, v6State string, sessionDead, freeMode bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State, d.Detail, d.V6State, d.SessionDead, d.FreeMode
}

// StateForUI 只要状态名（托盘 tick 循环判迁移用，避免裸读共享字段）。
func (d *Daemon) StateForUI() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.State
}

// ChannelViews 多通道观察读数（面板/回路快照用）。未接线观察器时返回空。
func (d *Daemon) ChannelViews() []ChannelView {
	if d.Channels == nil {
		return nil
	}
	return d.Channels.Views()
}

// SetAutoReconnect 切换自动重连开关（面板/托盘写，Tick 读——原子交接）。
func (d *Daemon) SetAutoReconnect(on bool) { d.autoReconnect.Store(on) }

// SetMultiChannel 切换通道级自愈开关（2026-11-20 双通道 M2）。默认关。
func (d *Daemon) SetMultiChannel(on bool) { d.multiChannel.Store(on) }

// MultiChannelOn 当前通道级自愈开关状态（快照/面板回显）。
func (d *Daemon) MultiChannelOn() bool { return d.multiChannel.Load() }

// AutoReconnectOn 当前开关状态（快照/菜单勾选回显）。
func (d *Daemon) AutoReconnectOn() bool { return d.autoReconnect.Load() }
