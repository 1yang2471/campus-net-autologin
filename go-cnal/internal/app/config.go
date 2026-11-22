package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/protocol"
)

// daemonConfig 守护配置面：读守护同款 ini（与 Python 守护 config.ini 同键名，
// [keeper] 节）。凭据绝不进配置文件——keyring 直读是唯一路径（t41 系红线）。
type daemonConfig struct {
	Server         string // 门户基址（必填）
	LoginPath      string // 登录端点路径（必填，校事实）
	LoginPage      string // 门户可达性探测页
	FluxPath       string // userflux 端点
	ProbeIPs       []string
	ProbePath      string
	ProbeHost      string
	HijackMarks    []string      // L1 探锚 200 型劫持页指纹
	// LoginPageMarks 门户登录页指纹（首跑探测 verifyPortalLogin 用）。空 = 默认
	// ac_portal/pwdLogin。异校必须可配：真机实测本校登录页正文**不含**这两串
	// （表单由 JS 渲染），默认指纹恒失败；本校稳定标记是 <title>上网认证系统</title>
	// 与 password_name（2026-11-11，见 docs/2026-11-11-根因修正与门户选源设计.md §5）。
	LoginPageMarks []string
	CampusDNS      string        // 校园 DNS（物理源选路探测地址，校事实）
	PortalDownWait time.Duration // 门户不可达时的重试/重发现周期
	// 拒绝分类退避（Python reject_backoff/transient_backoff 可配；异校防风暴
	// 阈值不同）。默认 600s/30s。
	RejectBackoff    time.Duration
	TransientBackoff time.Duration
	// Response 异校响应词表（[response] 段；零值字段回落默认，Python 同语义）。
	Response keeper.ResponseMarks
	// LoginForm 登录表单模板（ac_portal 族异校变体：字段名/操作名/静态参数
	// 可配；缺省 = 参考校默认，与 Python FORM_BASE 同形）。
	LoginForm     protocol.Form
	ProbeTimeout  time.Duration
	Interval      time.Duration
	FreeWindows   string
	Username      string // 当前账号（多账号切换 = 改它 + 读对应凭据）
	AutoReconnect bool   // 自动重连开关（false = 只探测报状态，不自动登录）
	// MultiChannel 通道级自愈开关（2026-11-20 双通道 M2）：默认**关**
	// （沿用 11-19「旁路只观察不登录」纪律）；开启后未认证的物理通道
	// 各自登录，与主链路共用账号级预算（8 次/小时 + 30s 间隔）。
	MultiChannel bool
	BindIP        string // 物理源逃生口：手工指定 v4 源（空 = 自动发现）
	// NetMode 门户选路模式（设计 §6）：auto=逐操作健康探测（绑源优先，绑源
	// 解析失败转系统栈）；bound=只走物理栈钉 IP；system=只走系统栈（跟 TUN）。
	NetMode      string
	DualStack    bool
	V6Peer       string
	PeerOpr      string
	PeerPath     string
	SSLVerify    bool
	UserAgent    string        // 登录/探测请求 UA（校事实；Python _headers 同源）
	LoginTimeout time.Duration // 登录 POST 独立超时（0 = 自适应 max(6,2×probe)）
	// 流量能力（只读）：低频轮询间隔与提醒阈值。零值 = 内核默认
	//（10 分钟 / 80,90）；异校门户限频与额度口径不同，须可配。
	FluxPollInterval time.Duration
	FluxWarnAt       float64
	FluxCriticalAt   float64
}

// loadDaemonConfig 读守护 ini：[keeper]/[portal] 扁平扫描 key = value，
// [response] 段单独消费词表（Python read_ini 同语义；[accounts] 的
// "1 = user|url" 键名不匹配任何守护键，天然忽略）。
func loadDaemonConfig(path string) (daemonConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return daemonConfig{}, err
	}
	defer f.Close()

	var c daemonConfig
	c.SSLVerify = true     // 安全默认：缺省校验证书，显式 ssl_verify=false 才放开
	c.AutoReconnect = true // Python DEFAULTS auto_reconnect=true：缺省开
	// 退避与词表默认值对齐 Python DEFAULTS/RESPONSE_DEFAULTS：参考校零改动，
	// 异校只覆盖自己踩到的键。
	c.RejectBackoff = 600 * time.Second
	c.TransientBackoff = 30 * time.Second
	c.Response = keeper.DefaultResponseMarks()
	c.LoginForm = protocol.DefaultForm()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// 记录节名：[response] 段单独消费（Python read_ini 同款）；
			// [keeper]/[portal] 仍扁平读取，与既有 ini 兼容。
			section = strings.Trim(strings.TrimSpace(line), "[]")
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if section == "response" {
			switch k {
			case "success_marks":
				c.Response.Success = keeper.SplitMarks(v)
			case "negative_marks":
				c.Response.Negative = keeper.SplitMarks(v)
			case "transient_marks":
				c.Response.Transient = keeper.SplitMarks(v)
			case "freeze_marks":
				c.Response.Freeze = keeper.SplitMarks(v)
			case "hijack_marks":
				// Python RESPONSE_DEFAULTS 把 hijack_marks 也放 [response]；
				// 从 Python 配置迁移时不得丢掉用户改过的劫持指纹。
				c.HijackMarks = keeper.SplitMarks(v)
			}
			continue
		}
		switch k {
		case "server":
			c.Server = v
		case "login_path":
			c.LoginPath = v
		case "login_page_marks":
			// 登录页指纹（异校必配：默认 ac_portal/pwdLogin 在 JS 渲染的门户
			// 登录页上恒不命中，见 detect.go 的 verifyPortalLogin）。
			c.LoginPageMarks = keeper.SplitMarks(v)
		case "login_form":
			f, err := protocol.ParseForm(v)
			if err != nil {
				return c, fmt.Errorf("login_form: %w", err)
			}
			c.LoginForm = f
		case "login_page":
			c.LoginPage = v
		case "flux_path":
			c.FluxPath = v
		case "probe_ips":
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					c.ProbeIPs = append(c.ProbeIPs, s)
				}
			}
		case "probe_path":
			c.ProbePath = v
		case "probe_host":
			c.ProbeHost = v
		case "campus_dns":
			c.CampusDNS = v
		case "portal_down_wait":
			n, err := strconv.Atoi(v)
			if err != nil {
				return daemonConfig{}, fmt.Errorf("portal_down_wait 不是整数: %w", err)
			}
			c.PortalDownWait = time.Duration(n) * time.Second
		case "reject_backoff":
			d, err := clampSeconds(k, v, 60, 86400) // Python _num 同款范围
			if err != nil {
				return daemonConfig{}, err
			}
			c.RejectBackoff = d
		case "transient_backoff":
			d, err := clampSeconds(k, v, 5, 600) // Python _num 同款范围
			if err != nil {
				return daemonConfig{}, err
			}
			c.TransientBackoff = d
		case "hijack_marks":
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					c.HijackMarks = append(c.HijackMarks, s)
				}
			}
		case "probe_timeout":
			n, err := strconv.Atoi(v)
			if err != nil {
				return daemonConfig{}, fmt.Errorf("probe_timeout 不是整数: %w", err)
			}
			c.ProbeTimeout = time.Duration(n) * time.Second
		case "interval":
			fl, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return daemonConfig{}, fmt.Errorf("interval 不是数字: %w", err)
			}
			c.Interval = time.Duration(fl * float64(time.Second))
		case "free_windows":
			c.FreeWindows = v
		case "username":
			c.Username = v
		case "dual_stack":
			c.DualStack = v == "true" || v == "1" || v == "on"
		case "auto_reconnect":
			c.AutoReconnect = !(v == "false" || v == "0" || v == "off")
		case "multi_channel":
			// 默认关：只有显式打开才启用通道级登录（保守方向）。
			c.MultiChannel = v == "true" || v == "1" || v == "on"
		case "bind_ip":
			c.BindIP = v
		case "net_mode":
			switch mode := strings.ToLower(v); mode {
			case NetModeAuto, NetModeBound, NetModeSystem:
				c.NetMode = mode
			default:
				return c, fmt.Errorf("net_mode %q 非法（只能是 auto/bound/system）", v)
			}
		case "user_agent":
			c.UserAgent = v
		case "login_timeout":
			n, err := strconv.Atoi(v)
			if err != nil {
				return daemonConfig{}, fmt.Errorf("login_timeout 不是整数: %w", err)
			}
			c.LoginTimeout = time.Duration(n) * time.Second
		case "flux_poll_interval_sec":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				continue // 非法/0 回落内核默认，不拦启动
			}
			c.FluxPollInterval = time.Duration(n) * time.Second
		case "flux_warn_at":
			parts := strings.Split(v, ",")
			if len(parts) != 2 {
				continue
			}
			warn, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			crit, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if e1 != nil || e2 != nil || warn <= 0 || crit <= warn || crit > 100 {
				continue // 非法回落默认，不拦启动
			}
			c.FluxWarnAt, c.FluxCriticalAt = warn, crit
		case "v6_peer":
			c.V6Peer = v
		case "peer_opr":
			c.PeerOpr = v
		case "peer_path":
			c.PeerPath = v
		case "ssl_verify":
			c.SSLVerify = !(v == "false" || v == "0" || v == "off")
		}
	}
	if err := sc.Err(); err != nil {
		return daemonConfig{}, err
	}
	// 默认值对齐 Python DEFAULTS（端点路径是校事实，缺省即守护同款默认）。
	// 返回已解析的部分配置：首跑 Setup 要用现有 login_path 等字段，不得因缺
	// server 就把整份配置当空（错误路径的其他调用方忽略 cfg 即可）。
	if c.Server == "" {
		return c, ErrMissingServer
	}
	if c.LoginPath == "" {
		return c, ErrMissingLoginPath
	}
	if c.LoginPage == "" {
		c.LoginPage = protocol.DefaultLoginPage
	}
	if c.FluxPath == "" {
		c.FluxPath = "/ac_portal/userflux"
	}
	if len(c.ProbeIPs) == 0 {
		c.ProbeIPs = defaultProbeIPs()
	}
	if c.ProbePath == "" {
		c.ProbePath = defaultProbePath
	}
	if c.ProbeHost == "" {
		c.ProbeHost = defaultProbeHost
	}
	if len(c.HijackMarks) == 0 {
		c.HijackMarks = []string{"ac_portal", "pwdLogin"}
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = 3 * time.Second
	}
	// 缺省与下限合一（2026-11-13 登录风暴实证：interval=1 配 probe_timeout=1
	// 是登录风暴脚枪——每秒一拍、单拍超时即判离线）：低于 3s 一律按 3s 跑。
	// 钳制是运行时下限，ini 原值保留不改写。
	if c.Interval < 3 * time.Second {
		c.Interval = 3 * time.Second
	}
	if c.PortalDownWait <= 0 {
		c.PortalDownWait = 30 * time.Second
	}
	if c.NetMode == "" {
		c.NetMode = NetModeAuto
	}
	if c.PeerPath == "" {
		c.PeerPath = "/"
	}
	// UA 缺省 = Python DEFAULTS 同款浏览器 UA（门户 WAF 会慢回复裸客户端）。
	// 唯一出处在 protocol.DefaultUserAgent（CLI 同源，不再各抄一份）。
	if c.UserAgent == "" {
		c.UserAgent = protocol.DefaultUserAgent
	}
	// 登录超时独立于 probe_timeout（Python P1-7：RADIUS 后端 3-6s 才回）。
	// 0 = 自适应 max(6s, 2×probe_timeout)。
	if c.LoginTimeout <= 0 {
		c.LoginTimeout = 6 * time.Second
		if 2*c.ProbeTimeout > c.LoginTimeout {
			c.LoginTimeout = 2 * c.ProbeTimeout
		}
	}
	return c, nil
}

// 门户选路模式（net_mode，设计 §6）。auto 是默认：绑源优先，绑源解析失败
// 才转系统栈；bound/system 是自动判错时的逃生口。
const (
	NetModeAuto   = "auto"
	NetModeBound  = "bound"
	NetModeSystem = "system"
)

// ErrMissingServer / ErrMissingLoginPath 首跑可修复的配置缺口（cnalw 据此
// 进入首个配置面板，而不是报错退出；其余配置错误仍须用户改 ini）。
var (
	ErrMissingServer    = errors.New("缺少 server")
	ErrMissingLoginPath = errors.New("缺少 login_path")
)

// clampSeconds 解析秒数并钳制到 Python _num 同款范围；非整数报错可见
// （静默回落会让用户以为配置生效了）。
func clampSeconds(key, v string, lo, hi int) (time.Duration, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s 不是整数: %w", key, err)
	}
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	return time.Duration(n) * time.Second, nil
}
