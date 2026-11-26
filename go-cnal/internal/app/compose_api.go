package app

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"cnal/internal/api"
	"cnal/internal/keeper"
)

// newAPI 装配回路 API 的全部缝（状态快照、手动登录、补绑、查流量、日志、
// 设置页读写与热生效）。cfg 传指针：面板保存账号会更新 cfg.Username，与
// 返回给 App.Cfg 的是同一个结构。
func newAPI(cfg *daemonConfig, p *plumbing, d *keeper.Daemon) *api.Server {
	a := api.New()
	// 查流量缝：面板/托盘 /flux 端点直通内核同款 flux 查询（同一 portal 客户端、
	// 同一 parseFluxTable——物理栈纪律与判死探针同源，不造第二份）。
	// 实时快照成功后回写守护缓存（2026-11-21 用户报告「流量明细与今日流量
	// 数值不一致」：明细实时 vs 今日流量缓存，时差制造两个数字——回写收敛）。
	a.FetchFlux = func() (map[string]string, error) {
		data, err := p.fluxQuery(*cfg)
		if err == nil {
			d.StoreFluxView(data)
		}
		return data, err
	}
	a.Snapshot = func() api.Snapshot {
		state, detail, v6State, sessionDead, freeMode := d.View()
		h := d.Health()
		lastKind, lastAt := d.LastAction()
		snap := api.Snapshot{
			State:   state,
			Detail:  detail,
			Label:   api.Label(state), // 词表在 api 包：闭包不自抄（漏填=图标空白）
			Color:   api.Color(state),
			V6State: v6State,
			V6Label: keeper.V6Label(v6State),
			V4Src:   srcStr(p.portal.Source()), // 重发现后读当前绑定源，不是启动时的旧值
			V6Src:   srcStr(p.currentV6()),     // 注入/轮换后的当前 v6 源，不重新枚举网卡
			Route:   p.routeText(),             // 当前门户路径（绑源钉 IP / 系统栈 + 原因）
			// 双读数（2026-11-18）：认证与用户路径分开报，不一致时并列显示。
			AuthState:   h.Auth,
			UserPath:    h.UserPath,
			Reason:      h.Reason,
			ReasonLabel: api.ReasonText(h.Reason),
			Action:      h.Action,
			// 未认证源（只展示；解释 Windows 黄叹号误报）：从通道读数派生——
			// 2026-11-20 起通道观察是唯一探测路径（旧旁路观察已删，避免同一
			// 张卡被探两次、两处规矩漂移）。契约字段保留：旧客户端零影响。
			UnauthSources: unauthChannelIPs(d.ChannelViews()),
			// 多通道认证态（2026-11-20 双通道 M1）：每张物理卡各自一份
			// （门户按源 IP 记会话）。label 由内核词表给出。
			Channels: d.ChannelViews(),
			// 动作时间线（2026-11-21 决议 C）：最近一次守护动作，面板显示
			//「N 分钟前·自动登录」——断网排查第一眼可读。
			LastAction:   lastKind,
			LastActionAt: lastAt,
			Extra: map[string]bool{"session_dead": sessionDead, "free_mode": freeMode,
				"auto_reconnect": d.AutoReconnectOn(),
				"multi_channel":  d.MultiChannelOn()},
		}
		if p.portal != nil {
			info := p.bindingInfo()
			snap.BindMode = info.Mode
			snap.BindInterface = info.Interface
			snap.BindIndex = info.Index
			snap.BindDegraded = info.Degraded
			if info.Err != nil {
				snap.BindError = info.Err.Error()
			}
		}
		// 原因覆盖状态名：本地节流不再冒充「登录被拒」，绑定失效不再只说「离线」。
		// fake_online 的覆盖即「会话在线｜出口被控」主状态词（2026-11-22 修复项 2）。
		if lbl, ok := api.ReasonLabel(h.Reason); ok {
			snap.Label = lbl
		}
		if c, ok := api.ReasonColor(h.Reason); ok {
			snap.Color = c
		}
		// 菜单行短形态（2026-11-22 修复项 4）：Reason 覆盖后的终值 Label +
		// 详情第一段；括号补充说明不进菜单行，完整读数留给 tooltip/面板。
		snap.MenuLine = api.MenuLine(snap.Label, snap.Detail, snap.UserPath)
		// 低频轮询缓存的流量视图：只在拿到过读数时带上（omitempty，旧客户端
		// 读到的 JSON 形状不变）。面板刷 /state 顺带取，不再单独打 /flux。
		if v := d.FluxUsage(); v.HasPercent || v.Used != "" || len(v.Raw) > 0 {
			snap.Flux = &v
		}
		return snap
	}
	a.Login = func() error { _, err := p.action.Do(); return err } // 手动：越过自动闸（Python manual=True 同语义）
	// v6 补绑：面板在 v6_state=offline（门户观察 isOnline=false）时显示的动作。
	// 与 a.Login 分开：DoRebind 握手失败会拒绝，不降级成假补绑。
	// 30s 去抖：按钮常驻可见，连点会带完整凭据反复登录撞门户反暴力冻结
	// （2026-11-08 发布审查 P1；手动重连的既有语义不动）。
	var rebindMu sync.Mutex
	var lastRebind time.Time
	a.RebindV6 = func() error {
		rebindMu.Lock()
		if time.Since(lastRebind) < 30*time.Second {
			rebindMu.Unlock()
			return errors.New("补绑太频繁：请 30 秒后重试（防门户反暴力冻结）")
		}
		lastRebind = time.Now()
		rebindMu.Unlock()
		return p.action.DoRebind()
	}
	// 可视化日志：面板 /logs 读状态日志尾部（200 行）——替代托盘「查看日志」
	// 直接开文件夹的占位实现（用户 2026-11-08 反馈：要可视化界面）。
	a.Logs = func() ([]string, error) { return TailLines(LogFilePath(), 200) }
	// 面板设置缝：GET 非敏感视图；POST 成对保存账号密码（凭据 per-user 落
	// compound，username/server/free_windows 落 ini）+ **热切换**——Relocate
	// 原子换 LoginAction 凭据，下一次登录（自动或手动）即用新账号，不重启进程。
	a.GetConfig = func() map[string]string {
		view := advancedConfigView(cfgPathCached) // 高级设置区（ini 已有字段）
		view["username"] = iniUsername()
		view["server"] = cfg.Server
		view["free_windows"] = d.FreeWindowsText() // 守护当前值，不是启动时的 cfg
		view["auto_reconnect"] = strconv.FormatBool(d.AutoReconnectOn())
		// multi_channel（2026-11-20）：漏了这个键时面板保存成功但回读拿不到，
		// 勾选被清成未勾选——用户感知「点多通道自愈再保存一直失败」。
		view["multi_channel"] = strconv.FormatBool(d.MultiChannelOn())
		view["bind_ip"] = cfg.BindIP
		return view
	}
	a.SaveCredentials = func(user, pass string) error {
		if err := credentialSource().Save(user, pass); err != nil {
			return err
		}
		// 热切换：引擎侧立即改用新账号（顺序：先落凭据再换——切换失败时
		// 凭据已保存，重试只差 ini 一行）。
		if err := p.action.Relocate(user, pass); err != nil {
			return err
		}
		setIniUsername(user) // 引擎内账号指向源同步
		cfg.Username = user
		RememberAccount(cfgPathCached, user, cfg.Server) // 历史账号置顶（失败不致命）
		return updateIniFields(map[string]string{"username": user})
	}
	// 账号历史：GET /accounts 列最近使用（current 标记指向当前账号）；
	// 切换 = keyring 按用户名还原密码 → 换凭据 → **立即认证一次** →
	// 把结果如实回报（取不到密码 → 拒绝，Python 同语义）。
	a.Accounts = func() []api.Account {
		cur := iniUsername()
		out := make([]api.Account, 0, 5)
		for _, acc := range ListAccounts(cfgPathCached) {
			out = append(out, api.Account{
				Username: acc.Username, Server: acc.Server, Current: acc.Username == cur,
			})
		}
		return out
	}
	a.SwitchAccount = func(user string) (string, error) {
		msg, err := switchAccount(user,
			credentialSource().Load,
			func(user, pass string) error {
				// 凭据一落地就把「当前账号」写进 ini：认证失败（门户冻结）也
				// 不影响下次启动用新账号——切换是切换，认证是认证。
				if err := p.action.Relocate(user, pass); err != nil {
					return err
				}
				setIniUsername(user)
				return updateIniFields(map[string]string{"username": user})
			},
			p.sessionOps(*cfg))
		// 切换结果落状态日志：面板消息一闪而过，事故复盘时日志是唯一现场
		//（2026-11-10 切账号失败无任何日志可查）。
		event := fmt.Sprintf("切换账号 %s: %s", user, msg)
		if err != nil {
			event = fmt.Sprintf("切换账号 %s 失败: %v", user, err)
		}
		if p.logf != nil {
			p.logf("%s\n", event)
		}
		// p.logf 在桌面是 stdout、在安卓是内存数组（LogsJSON），两者都不进
		// cnalw.log——2026-11-12 真机踢线实证：切账号成功但状态日志无行。
		// 补一份到状态日志文件，与状态迁移同文件可对齐。
		if err := AppendEvent(LogFilePath(), EventClock(), event); err != nil && p.logf != nil {
			p.logf("状态日志追加失败: %v\n", err)
		}
		if err != nil {
			return "", err
		}
		cfg.Username = user
		RememberAccount(cfgPathCached, user, cfg.Server) // 历史账号置顶（失败不致命）
		// 切账号成功：作废流量视图缓存（旧账号读数不得显示在新账号名下；
		// 下一拍 Tick 立即重查——2026-11-20 用户报告「今日流量与明细不一致」）。
		d.InvalidateFlux()
		// 动作时间线（2026-11-21 决议 C）：切账号也是守护动作，面板可追溯。
		d.RecordAction("切换账号")
		return msg, nil
	}
	// 面板设置校验（api /config POST 在任何写入之前调用）：免认证时段必须
	// 严格合法——解析器对旧配置容错，但用户新输入不得被静默吞掉。
	a.ValidateConfig = func(values map[string]string) error {
		if v, ok := values["free_windows"]; ok {
			if err := keeper.ValidateFreeWindows(v); err != nil {
				return err
			}
		}
		// 高级键单独校验；常用键不传进来（白名单外会误报）。
		advanced := map[string]string{}
		for k, v := range values {
			if advancedKeeperKeys[k].hi > 0 || advancedTextKeys[k] || advancedResponseKeys[k] {
				advanced[k] = v
			}
		}
		return validateAdvancedConfig(advanced)
	}
	a.SaveConfig = func(values map[string]string) error {
		// 先校验、再落 ini、最后才改内存热设置——保存失败时运行态不得半改。
		// 统一分派（常用键/高级键分节写），与首跑面板共用同一条路径。
		if err := saveConfigValues(values); err != nil {
			return err
		}
		if v, ok := values["auto_reconnect"]; ok {
			d.SetAutoReconnect(v == "true" || v == "1" || v == "on")
		}
		if v, ok := values["multi_channel"]; ok {
			d.SetMultiChannel(v == "true" || v == "1" || v == "on")
		}
		if v, ok := values["free_windows"]; ok {
			d.SetFreeWindows(v) // 内存与 ini 同时到位，面板回读也是新值
		}
		return nil
	}
	return a
}
