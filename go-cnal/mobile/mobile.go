// Package mobile 是安卓 Kotlin 壳与 Go 内核之间的桥（gomobile bind → AAR）。
//
// 约束（go-cnal/docs/android-plan.md §3.1）：gobind 不适合直出 app.App 这种
// 带函数字段的结构，所以这里只暴露 JSON 字符串 / error / 基础类型；Kotlin
// 侧只依赖本文件里的方法，内部仍走 app.Compose 与同一套回路 API 契约。
//
// 生命周期：
//
//	SetDataDir / SetCredentials / SetPhysicalSources / SetPanelPage / SetSourceLog
//	→ Start(configPath) → StateJSON/Login/FluxJSON/LogsJSON/... → Stop()
//
// Start 内部起与 cmd/cnald 相同的 tick goroutine（Daemon.Tick + Interval 睡眠）；
// 前台服务只负责让进程活着，不参与状态机。凭据由 Kotlin Keystore 解密后经
// SetCredentials 注入，只住内存，任何文件都不落密码。
package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cnal/internal/api"
	"cnal/internal/app"
	"cnal/internal/netstack"
)

var (
	mu      sync.Mutex
	running *app.App
	stopCh  chan struct{}
	wg      sync.WaitGroup
	creds   = &memoryCredentials{data: map[string]string{}}
	// wakeCh 事件驱动唤醒通道（复盘 §3.2）：Kotlin NetworkCallback 回调
	// 线程经 Wake() 投递，beatLoop 消费。Start 建新通道，Stop 置 nil。
	wakeCh chan string
	// sourceLog 收集物理源/重发现打印（安卓 stdout 不可见），供 LogsJSON 用。
	sourceLogMu sync.Mutex
	sourceLog   []string
	// detectV4 最近一次注入的物理 v4（首跑门户探测钉源用；空 = 尚未注入）。
	detectV4Mu sync.Mutex
	detectV4   string
)

// Wake 事件驱动唤醒（AAR 导出，Kotlin NetworkCallback 调）：打断 beatLoop
// 的兜底等待立即做一拍完整 Tick。reason 只进日志语义（onLost/onAvailable/
// onCapabilitiesChanged/onLinkPropertiesChanged），通道满/未启动时丢弃——
// Wake 是「尽快 Tick」加速器，丢弃不损正确性（兜底周期仍在）。
func Wake(reason string) {
	mu.Lock()
	ch := wakeCh
	started := running != nil
	mu.Unlock()
	if ch == nil || !started {
		return // 未启动 no-op；通道满由 postWake 丢弃
	}
	postWake(ch, reason)
}

// memoryCredentials 安卓凭据源：Kotlin 解密后把明文交给 Go，只在内存。
// 进程被杀即消失，重启后由 Kotlin 重新注入（Keystore 密文在 app 私有目录）。
type memoryCredentials struct {
	mu       sync.Mutex
	data     map[string]string
	user     string
	pass     string
	hasEntry bool
	sink     CredentialSink
}

// CredentialSink 安卓 Keystore 落盘缝（Kotlin 实现；gomobile 绑定成 Java
// 接口）。只在面板保存（CredentialSource.Save）时调用；启动注入走 Put，
// 不回调——否则每次启动都把全部账号重加密一遍。
type CredentialSink interface {
	Save(user, pass string) error
}

func (m *memoryCredentials) Load(user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.data[user]; ok {
		return p, nil
	}
	return "", errors.New("凭据库无此账号")
}

func (m *memoryCredentials) LoadLegacy() (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasEntry {
		return m.user, m.pass, nil
	}
	return "", "", errors.New("无单条旧凭据")
}

// Save 面板保存路径：先落 Kotlin（Keystore），成功后才进内存。
// 落盘失败必须整体失败——否则本次运行能用、重启就丢。
func (m *memoryCredentials) Save(user, pass string) error {
	if user == "" || pass == "" {
		return errors.New("账号或密码为空")
	}
	m.mu.Lock()
	sink := m.sink
	m.mu.Unlock()
	if sink != nil {
		if err := sink.Save(user, pass); err != nil {
			return err
		}
	}
	return m.Put(user, pass)
}

// Put 只写内存（启动注入路径；不触发 Keystore 回调）。
func (m *memoryCredentials) Put(user, pass string) error {
	if user == "" || pass == "" {
		return errors.New("账号或密码为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[user] = pass
	m.user, m.pass, m.hasEntry = user, pass, true
	return nil
}

// Current 当前（最后注入）账号：启动时补 ini 账号指向源与历史账号用。
func (m *memoryCredentials) Current() (string, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user, m.pass, m.hasEntry
}

// SetDataDir 注入 context.filesDir（config/log/api.json 都落这里）。
// 必须在 Start 之前调用。
func SetDataDir(dir string) {
	app.SetDataDir(dir)
	app.SetCredentialSource(creds)
	// 安卓 stdout 不可见：把物理源/重发现打印收进内存，LogsJSON 一起带出。
	app.SetSourcePrinter(func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		sourceLogMu.Lock()
		sourceLog = append(sourceLog, strings.TrimRight(line, "\n"))
		if len(sourceLog) > 200 {
			sourceLog = sourceLog[len(sourceLog)-200:]
		}
		sourceLogMu.Unlock()
	})
}

// SetCredentials 注入当前账号与密码（Kotlin Keystore 解密后调用，只进内存）。
// 启动注入不触发 Keystore 落盘回调（见 CredentialSink 注释）。
func SetCredentials(user, pass string) error { return creds.Put(user, pass) }

// SetCredentialSink 注入 Keystore 落盘实现（Kotlin 侧实现 Mobile 生成的
// Java 接口）。面板保存账号时先落盘再进内存；nil = 不落盘（桌面内核不用）。
func SetCredentialSink(s CredentialSink) {
	creds.mu.Lock()
	creds.sink = s
	creds.mu.Unlock()
}

// SetPhysicalSources 注入 wlan0 的 v4 与全局 GUA v6（v6 可传空串 = 只 v4）。
// v6 临时地址轮换时 Kotlin 重新调本方法，面板与握手立即看到新地址。
func SetPhysicalSources(v4, v6 string) error {
	var ip4, ip6 net.IP
	if v4 != "" {
		ip4 = net.ParseIP(v4)
		if ip4 == nil || ip4.To4() == nil {
			return fmt.Errorf("v4 地址非法: %q", v4)
		}
	}
	if v6 != "" {
		ip6 = net.ParseIP(v6)
		if ip6 == nil || ip6.To4() != nil || !ip6.IsGlobalUnicast() {
			return fmt.Errorf("v6 不是全局 GUA: %q", v6)
		}
	}
	app.SetPhysicalSources(app.PhysicalSources{V4: ip4, V6: ip6})
	detectV4Mu.Lock()
	detectV4 = v4 // 空串 = 清掉，探测会拒绝（物理栈纪律）
	detectV4Mu.Unlock()
	return nil
}

// DetectPortal 首跑门户探测（AAR 导出；Kotlin 先绑 Wi-Fi 并 SetPhysicalSources）。
//
// 与 cnal detect 同语义：未认证时公网锚点被 302 劫持，Location 自报门户地址，
// 再拉登录页复核指纹；全程无凭据 GET。configPath 可为空（全默认）；存在时只
// 读探测相关键（校园 DNS/锚点/登录页指纹）。
//
// 要求已注入物理 v4：安卓上 "自动发现源" 可能选到 rmnet/VPN 而不是已绑定的
// Wi-Fi（物理栈纪律，见 AGENTS「探测/登录必须走物理栈」）；没有注入源时直接
// 报错，不猜出口、不触网。
func DetectPortal(configPath string) (string, error) {
	detectV4Mu.Lock()
	src := detectV4
	detectV4Mu.Unlock()
	if src == "" {
		return "", errors.New("未注入物理 v4 源：先绑 Wi-Fi 再探测（物理栈纪律，不猜出口）")
	}
	opt := app.DetectOptionsFromConfig(configPath, src)
	return app.DetectPortal(opt)
}

// SetPanelPage 注入单文件面板 HTML（cmd/cnalw 的 panel.html/css/js 拼好的
// 自包含页）。不设则 /panel 返回 404，但其余端点与守护照常工作。
var (
	panelPageMu sync.RWMutex
	panelPage   string
)

func SetPanelPage(page string) {
	panelPageMu.Lock()
	defer panelPageMu.Unlock()
	panelPage = page
}

func currentPanelPage() string {
	panelPageMu.RLock()
	defer panelPageMu.RUnlock()
	return panelPage
}

// Start 装配内核并起 tick 循环，然后启动回路 API，返回面板 URL
// （http://127.0.0.1:<port>/panel?token=...）。Kotlin WebView 直接加载它。
// 重复 Start 返回错误；失败时保持未启动状态，可再次调用。
func Start(configPath string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		return "", errors.New("内核已在运行（先 Stop）")
	}
	if configPath == "" {
		return "", errors.New("configPath 为空")
	}
	if _, err := os.Stat(configPath); err != nil {
		return "", fmt.Errorf("配置文件不可读: %w", err)
	}
	a, err := app.Compose(configPath)
	if err != nil {
		return "", err // SetupError 的 Reason 会带出「配置未就绪/缺凭据」
	}
	// 账号历史种子：安卓首跑 ini 只有 server/login_path，没有 username 与
	// [accounts]，面板「历史账号」下拉会是空的，/config 回显账号也是空。
	// 把当前注入账号补成第一条（幂等；面板/桌面路径已写过就是原样）。
	cur := a.Cfg.Username
	if cur == "" {
		if u, _, ok := creds.Current(); ok {
			cur = u
		}
	}
	if cur != "" {
		if a.Cfg.Username == "" {
			if err := app.SetIniUsername(cur); err != nil {
				recordLog("账号指向源写入失败: " + err.Error())
			}
		}
		if err := app.RememberAccount(configPath, cur, a.Cfg.Server); err != nil {
			recordLog("账号历史写入失败: " + err.Error())
		}
	}
	// 免费时段判定用注入的时区偏移（gomobile 无 TZ，time.Now 挂 UTC——
	// 2026-11-11 首跑实证：UTC 下午被判进 23:30-06:30 免认证窗口）。
	a.Daemon.LocalMinute = localMinuteFromTZ
	a.API.PanelPage = currentPanelPage()
	addr, err := a.API.Listen()
	if err != nil {
		return "", fmt.Errorf("回路 API 启动失败: %w", err)
	}
	// 端点文件：与 Windows 同一份 RuntimeInfo 契约，方便 adb 验收脚本读。
	cleanup, err := app.WriteRuntimeFile(app.RuntimeFilePath(), "http://"+addr.String(), a.API.Token)
	if err != nil {
		// 端点文件写不了不影响内核（cnal status 不可用而已），但要可见。
		recordLog("端点文件写入失败: " + err.Error())
		cleanup = func() {}
	}
	// 状态迁移日志：落到注入目录下的 cnalw.log，面板 /logs 与 LogsJSON 同源。
	// RotateWriter：心跳行 3s/拍 ≈1.6MB/天，5MB+5MB 备份封顶磁盘占用（手机存储紧）。
	logPath := app.LogFilePath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	rw, rerr := app.NewRotateWriter(logPath, 5*1024*1024)
	if rerr != nil {
		recordLog("状态日志不可写: " + rerr.Error())
	} else {
		// 空窗自报（2026-11-13 停搏无痕教训，与托盘同一函数）：进程被杀/深Doze
		// 冻结后重启，把「上次拍距现在多远」写成一行——安卓日志不可见，这行随
		// LogsJSON 带出。时钟用 tzNow（gomobile UTC 陷阱，同 recordLog 纪律）。
		app.AnnotateHeartbeatGap(logPath, rw, tzNow, 30*time.Minute)
		// 日志时间戳用 tzNow（注入偏移渲染设备本地时间）：time.Now 在
		// gomobile 里挂 UTC，真机日志会和手机时钟差 8 小时（2026-11-11 实证）。
		a.Logger = app.NewTransitionLogger(rw, tzNow)
		a.Heart = app.NewHeartbeatLogger(rw, tzNow) // 每拍必落行（A1 漏拍测量）
		a.LogFile = rw                              // recordLog 落「唤醒:」行（事件对照证据）
		app.SetEventClock(tzNow) // 切账号等事件行与状态迁移同用设备本地时间
	}
	stopCh = make(chan struct{})
	wakeCh = make(chan string, wakeBuffer)
	running = a
	wg.Add(1)
	go beatLoop(a, stopCh, wakeCh, rw, cleanup)
	return a.API.PanelURL(), nil
}

// beatFloor 事件驱动的兜底周期（复盘分支 5 拍板）：门户静默踢线时 Wi-Fi
// 不断、NetworkCallback 不响，只有兜底探针能发现。5min 内平均发现延迟
// 2.5min——A1 严格判据若因此不过，按测试计划 §1 降级条款如实降级。
const beatFloor = 5 * time.Minute

// beatLoop 事件驱动主循环：Tick → 等待（回调唤醒 or 5min 兜底）→ Tick。
// 配置 interval 仅在免认证窗口派上用场（免流时段低频兜底），其余时间以
// 事件为主、beatFloor 为底。
func beatLoop(a *app.App, stop <-chan struct{}, wake <-chan string,
	logFile *app.RotateWriter, cleanup func()) {
	defer wg.Done()
	defer cleanup()
	if logFile != nil {
		defer logFile.Close()
	}
	interval := a.Cfg.Interval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	recordLog("事件驱动: 回调唤醒 + 兜底 " + beatFloor.String())
	for {
		// Tick 前先消费一个已积压的唤醒（连续多路合并成一拍）。
		select {
		case <-stop:
			return
		case r := <-wake:
			recordLog("唤醒: " + r)
		default:
		}
		a.Daemon.Tick()
		if a.Logger != nil {
			state, detail, _, _, _ := a.Daemon.View()
			a.Logger.Observe(state, detail) // 迁移才写（内部去重）
		}
		if a.Heart != nil {
			state, detail, _, _, _ := a.Daemon.View()
			a.Heart.Beat(state, detail) // 每拍必写（A1 漏拍测量）
		}
		// 等待下一拍：唤醒打断 or 兜底到期。stop 优先级最高。
		select {
		case <-stop:
			return
		default:
		}
		beatWait(stop, wake, beatFloor)
	}
}

// Stop 停 tick 循环并让回路 API 停止接受新请求。幂等。
func Stop() {
	mu.Lock()
	if running == nil {
		mu.Unlock()
		return
	}
	ch := stopCh
	running, stopCh = nil, nil
	wakeCh = nil
	mu.Unlock()
	close(ch)
	wg.Wait()
}

// StateJSON 返回 /state 同构的 JSON 快照（state/detail/label/color/v6_*）。
// 与 HTTP /state 端点同构：Label/Color 空时按状态词表补全，否则首拍前的
// starting 态会给前端空标签/空色（面板大卡显示空白）。
func StateJSON() string {
	mu.Lock()
	a := running
	mu.Unlock()
	if a == nil || a.API.Snapshot == nil {
		return notRunningSnapshot()
	}
	snap := a.API.Snapshot()
	if snap.State == "" {
		// 守护刚装配、首拍还没跑：HTTP /state 会给空态，但面板启动期不应
		// 显示空白大卡——与词表 starting 对齐（M0 面板同款启动态）。
		snap.State = "starting"
		snap.Detail = "启动中（首拍检测前）"
	}
	if snap.Label == "" {
		snap.Label = api.Label(snap.State)
	}
	if snap.Color == "" {
		snap.Color = api.Color(snap.State)
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return `{"state":"offline","detail":"状态序列化失败"}`
	}
	return string(b)
}

func notRunningSnapshot() string {
	b, _ := json.Marshal(api.Snapshot{
		State:  "starting",
		Detail: "守护未启动",
		Label:  api.Label("starting"),
		Color:  api.Color("starting"),
	})
	return string(b)
}

// SetAutoReconnect 热切换自动重连开关（A4 VPN 降级解：Kotlin 检测 VPN 边沿后调）。
// 只改 daemon 内存开关，不写 ini——VPN 暂停是临时状态，断线恢复后 Kotlin 会调回
// 用户原设定；写盘会在进程被杀时把临时暂停固化成永久关闭（2026-11-18 教训）。
// 未启动时 no-op（VPN 回调可能早于 Start 到达）。
func SetAutoReconnect(on bool) {
	mu.Lock()
	a := running
	mu.Unlock()
	if a == nil {
		return
	}
	a.Daemon.SetAutoReconnect(on)
}

// Login 手动重连（越过自动闸与速率熔断，与面板「立即检测重连」同语义）。
func Login() error {
	mu.Lock()
	a := running
	mu.Unlock()
	if a == nil || a.ManualLogin == nil {
		return errors.New("守护未启动")
	}
	return a.ManualLogin()
}

// RebindV6 v6 补绑（dual_stack=false 时后端拒绝，语义与桌面一致）。
func RebindV6() error {
	mu.Lock()
	a := running
	mu.Unlock()
	if a == nil || a.API.RebindV6 == nil {
		return errors.New("守护未启动")
	}
	return a.API.RebindV6()
}

// FluxJSON 查流量，返回 /flux 同构的 JSON 对象；失败带 error。
func FluxJSON() (string, error) {
	mu.Lock()
	a := running
	mu.Unlock()
	if a == nil || a.API.FetchFlux == nil {
		return "", errors.New("守护未启动")
	}
	data, err := a.API.FetchFlux()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LogsJSON 返回状态日志尾部 200 行 + 物理源/重发现打印的合并 JSON 数组。
func LogsJSON() string {
	mu.Lock()
	a := running
	mu.Unlock()
	lines := []string{}
	if a != nil && a.API.Logs != nil {
		if got, err := a.API.Logs(); err == nil {
			lines = append(lines, got...)
		}
	}
	sourceLogMu.Lock()
	lines = append(lines, sourceLog...)
	sourceLogMu.Unlock()
	if len(lines) > 200 {
		lines = lines[len(lines)-200:]
	}
	b, _ := json.Marshal(lines)
	return string(b)
}

// SwitchAccount 切换账号（注入的凭据库须有该账号的密码，否则拒绝）。
func SwitchAccount(user string) error {
	mu.Lock()
	a := running
	mu.Unlock()
	if a == nil || a.API.SwitchAccount == nil {
		return errors.New("守护未启动")
	}
	_, err := a.API.SwitchAccount(user)
	return err
}

// recordLog 把启动期的可见事件记进内存日志（LogsJSON 一并带出）。
// 若状态日志文件已挂 RotateWriter（beatLoop 运行期），同步落一份到
// cnalw.log——唤醒/兜底行是 A1 漏拍判定的对照证据，手机一拔就只剩文件。
func recordLog(line string) {
	sourceLogMu.Lock()
	sourceLog = append(sourceLog, line)
	if len(sourceLog) > 200 {
		sourceLog = sourceLog[len(sourceLog)-200:]
	}
	var rw *app.RotateWriter
	mu.Lock()
	if running != nil {
		rw = running.LogFile
	}
	mu.Unlock()
	sourceLogMu.Unlock()
	if rw != nil {
		// 时间戳跟 tzNow（设备本地渲染）——gomobile time.Now 挂 UTC，裸用
		// 会在真机日志里与手机时钟差 8 小时（2026-11-11 实证、2026-11-13
		// 凌晨 A1 轮复发：启动行标 17:57 实为本地 01:57）。
		_, _ = fmt.Fprintf(rw, "%s INFO %s\n", tzNow().Format("2006-01-02 15:04:05"), line)
	}
}

// EgressCheck A4 绑网核对：用**注入的物理源**向门户问「你看到我的地址是什么」。
//
// 为什么需要它（android-a1-a5-testplan §4）：A4 的通过线是「门户侧看到的出口
// IP = 校园网出口」，而 M0 的探针只记状态码，看不到出口 IP。这里直接打门户的
// `opr=ipv4or6online`（**无凭据**，对端只回报请求源地址），Go 侧绑源发出——
// 它就是 M1 登录用的那条路（Go native socket + LocalAddr），因此能证明
// 「Go 的 socket 在 bindProcessToNetwork 下确实走 Wi-Fi」。
//
// insecure 取配置里的 ssl_verify（false = 跳过 CA 链但仍校验门户主机名）。
// 安卓必须支持它：真机实测 Android 系统信任库 149 个 CA 里**没有**本校门户的
// 签发 CA（Xcc Trust OV SSL），同一张证书在 Windows 上能过、在 Android 上
// CertPathValidatorException——跳过链校验 + 主机名校验是可行解。
//
// loginURL 由 Kotlin 拼好（server + login_path），必须 http(s)://host。
func EgressCheck(loginURL string, insecure bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(loginURL))
	if err != nil || u.Host == "" {
		return "", errors.New("门户登录地址非法")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("门户登录地址必须是 http(s)")
	}
	src := app.CurrentPhysicalSources().V4
	if src == nil {
		return "", errors.New("未注入物理源（Kotlin 侧先 SetPhysicalSources）")
	}
	c := netstack.New(src, 8*time.Second)
	if insecure {
		c = c.InsecureTLS() // 跳过 CA 链；门户主机名仍强制校验
	}
	return c.Post(u.String(), map[string]string{"opr": "ipv4or6online"}, nil)
}
