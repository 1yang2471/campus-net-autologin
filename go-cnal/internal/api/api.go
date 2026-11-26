// Package api 回路契约（设计规格 §4）：仅监听 127.0.0.1、启动时随机 token。
// GET /state 状态快照、POST /login 手动重连。词表与 Python kernel_api 逐字
// 对齐——两份外壳（Windows 托盘 / 安卓 WebView 面板）吃同一契约，「全功能
// 对齐」的判据是契约字段与动作集合一致，不是像素一致。
// 词表与渲染规则住在 labels.go（2026-11-22 拆分，守 2 万字节提交闸）。
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"cnal/internal/keeper"
)

// Snapshot 状态快照（契约字段：state/detail/label/color + 布尔扩展位 +
// v6 四态）。v6_label 由装配层填（文案唯一出处在内核 keeper.V6Label）。
type Snapshot struct {
	State   string `json:"state"`
	Detail  string `json:"detail"`
	Label   string `json:"label"`
	Color   string `json:"color"`
	// MenuLine 托盘菜单行短文案（2026-11-22 修复项 4，单源渲染）：内核单处
	// 生成（compose_api 填），菜单行吃短形态、tooltip/面板吃全量 Detail——
	// Detail 全量进菜单行会把托盘菜单撑到几十字宽（11-22 用户点名）。
	// omitempty：旧客户端读到的 JSON 形状不变。
	MenuLine string `json:"menu_line,omitempty"`
	V6State  string `json:"v6_state,omitempty"`
	V6Label  string `json:"v6_label,omitempty"`
	// 物理源读数（面板 IPv4/IPv6 行显示实际绑定源——TUN/代理环境下
	// 「从哪张网卡出去」是独有信息，不与状态大卡重复）。装配层填，空=未知。
	V4Src string `json:"v4_src,omitempty"`
	V6Src string `json:"v6_src,omitempty"`
	// Route 门户路径读数（面板「门户路径」行）："绑源钉 IP（钉 1.1.1.2:443）"
	// 或 "系统栈（绑源解析失败…，转系统栈）"。空 = 还没选过路。
	Route string `json:"route,omitempty"`
	// 物理栈绑定诊断：interface=接口级绑定，source_only=仅绑源 IP。
	// Degraded=true 表示严格模式关闭后发生了降级；旧客户端可忽略。
	BindMode      string `json:"bind_mode,omitempty"`
	BindInterface string `json:"bind_interface,omitempty"`
	BindIndex     int    `json:"bind_index,omitempty"`
	BindDegraded  bool   `json:"bind_degraded,omitempty"`
	BindError     string `json:"bind_error,omitempty"`
	// 双读数（2026-11-18）：认证读数（物理栈）+ 用户路径读数（系统栈）。
	// 旧客户端读不到这些键（omitempty）；面板/托盘按需显示。
	AuthState   string          `json:"auth_state,omitempty"`
	UserPath    string          `json:"user_path,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	ReasonLabel string          `json:"reason_label,omitempty"`
	Action      string          `json:"action,omitempty"`
	// UnauthSources 其他物理网卡上「已连接但未认证」的源（只观察，不登录）。
	// 多网卡下 Windows 联网指示取最差结果 → 黄叹号误报；面板据此把事实说清。
	// 空 = 无旁路卡/全部已认证/无法判定（omitempty 保证旧客户端零影响）。
	UnauthSources []string `json:"unauth_sources,omitempty"`
	// Channels 各物理通道的认证态（2026-11-20 双通道 M1）：门户按源 IP 记
	// 会话，每张卡各自一份。字段与内核 ChannelView 一一对应；状态词由内核
	// 给出（面板不自造状态词）。空 = 未观察/无其它通道。
	Channels []keeper.ChannelView `json:"channels,omitempty"`
	Extra    map[string]bool      `json:"extra,omitempty"`
	// Flux 流量用量只读视图（低频轮询缓存；面板刷 /state 顺带取，不再单独
	// 打 /flux）。nil = 未接线/还没查过，omitempty 保证旧客户端零影响。
	Flux *keeper.FluxUsageView `json:"flux,omitempty"`
	// LastAction/LastActionAt 最近一次守护动作（2026-11-21 决议 C）：面板
	// 状态卡显示「N 分钟前·自动登录」——断网排查时用户第一眼知道守护
	// 动没动过。kind 空 = 进程内还没动作过（旧客户端零影响）。注意
	// last_action_at 是值类型：omitempty 对 time.Time 不生效，零值时刻
	// 恒序列化——旧客户端按 last_action 的有无判即可（trouble-maker P2-1）。
	LastAction   string    `json:"last_action,omitempty"`
	LastActionAt time.Time `json:"last_action_at,omitempty"`
}

// Account 历史账号（多账号切换；Python [accounts] 段同语义）。
type Account struct {
	Username string `json:"username"`
	Server   string `json:"server"`
	Current  bool   `json:"current"`
}

// Server 回路 API。Snapshot/Login 是缝：生产注入内核读数与手动重连动作，
// 测试注入替身。Token 构造时生成（32 hex 字符）。PanelPage 非空时 /panel
// 以 text/html 伺服该页（go:embed 的单文件面板；Windows 浏览器与安卓
// WebView 吃同一份页料）。
type Server struct {
	Token     string
	PanelPage string
	Snapshot  func() Snapshot
	Login     func() error
	// 配置缝（面板设置页；nil = 未接线，/config 返回 501）：
	// GetConfig 返回非敏感配置视图（密码绝不回传）；
	// SaveCredentials 落凭据存储；SaveConfig 改写 ini。
	GetConfig       func() map[string]string
	SaveCredentials func(user, pass string) error
	SaveConfig      func(values map[string]string) error
	// ValidateConfig 面板提交前的业务校验（nil = 不校验）：返回 error → 400。
	// 必须在任何凭据/ini 写入之前调用——防「密码已保存、设置被拒」的半写。
	ValidateConfig func(values map[string]string) error
	// FetchFlux 查流量缝（nil = 未接线，/flux 返回 501）。语义同 Python
	// kernel_api.fetch_flux：成功返回 HTML 表解析出的键值对，失败返回 err
	// （→ 502，不造假数据）。与 keeper.Daemon.FetchFlux 同名——组合根接线
	// 是一次转发，不造第二套词汇。
	FetchFlux func() (map[string]string, error)
	// 账号历史缝（nil = 未接线，/accounts 返回 501）：Accounts 列历史
	// （最近使用在前 + current 标记）；SwitchAccount 按用户名从 keyring 还原
	// 密码 → 换凭据 → **立即认证一次**，成功返回给用户看的结果文案（切换
	// 成功但认证被门户拒绝也走成功路径——账号确实换了），取不到密码/写盘
	// 失败 → error（Python「必须拒绝切换」同语义）。
	Accounts      func() []Account
	SwitchAccount func(username string) (string, error)
	// RebindV6 补绑 v6 动作缝（nil = 未接线，/rebindv6 返回 501）：双栈开且
	// 门户观察 isOnline=false 时，用当前 v6 源地址主动登录一次（带 ipv4or6）。
	// 与 /login 分开是为了让面板能据 v6_state 控制按钮可用性、失败带补绑上下文。
	RebindV6 func() error
	// Logs 日志尾部缝（nil = 未接线，/logs 返回 501）：返回最近若干行，
	// 新的在最后（与文件追加顺序一致）。读失败返回 error → 502。
	Logs func() ([]string, error)

	addr net.Addr // Listen 时记录（PanelURL 用）
}

// New 生成带随机 token 的 Server（crypto/rand，16 字节熵）。
func New() *Server {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("随机源不可用: " + err.Error())
	}
	return &Server{Token: hex.EncodeToString(buf)}
}

// Handler 返回路由（token 中间件 + /state /login /panel /config 四端点）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/state", s.auth(s.state))
	mux.HandleFunc("/login", s.auth(s.login))
	mux.HandleFunc("/panel", s.auth(s.panel))
	mux.HandleFunc("/config", s.auth(s.config))
	mux.HandleFunc("/flux", s.auth(s.flux))
	mux.HandleFunc("/accounts", s.auth(s.accounts))
	mux.HandleFunc("/rebindv6", s.auth(s.rebindv6))
	mux.HandleFunc("/logs", s.auth(s.logs))
	return mux
}

// Listen 在 127.0.0.1 随机端口开始伺服（后台 goroutine），返回实际地址。
// 回路契约不落盘、不外发；地址只给本机 UI 拼接 URL 用。超时全部显式——
// 慢连接不得长期占住回路服务（Mimosa 建议采纳）。
func (s *Server) Listen() (net.Addr, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s.addr = ln.Addr()
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr(), nil
}

// PanelURL 面板完整地址（含 token query——URL 即凭据，只给本机 UI 用）。
// 未 Listen 过返回空串。
func (s *Server) PanelURL() string {
	if s.addr == nil {
		return ""
	}
	return "http://" + s.addr.String() + "/panel?token=" + s.Token
}

// auth 校验 Authorization: Bearer <token>；?token= 仅放行 /panel（浏览器
// 顶层导航设不了自定义头）。状态变更端点（/config /accounts /rebindv6 /
// /login）只收 Bearer 头：URL 里的 token 会进浏览器历史/Referer，不能被
// 拿去改配置或触发登录（2026-11-08 发布审查 P2）。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok := r.Header.Get("Authorization") == "Bearer "+s.Token
		if !ok && r.URL.Path == "/panel" {
			ok = r.URL.Query().Get("token") == s.Token
		}
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// state GET /state：状态快照 + 词表翻译。
func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	if s.Snapshot == nil {
		http.Error(w, "状态缝未接线", http.StatusNotImplemented)
		return
	}
	snap := s.Snapshot()
	if snap.Label == "" {
		snap.Label = stateLabels[snap.State]
	}
	if snap.Color == "" {
		snap.Color = stateColors[snap.State]
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(snap)
}

// login POST /login：手动重连（手动意图越过自动闸），返回 ok/失败原因。
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.Login == nil {
		http.Error(w, "登录缝未接线", http.StatusNotImplemented)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.Login(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// panel GET /panel：伺服内嵌单文件面板页；未设置页料 → 404（无头壳不崩）。
func (s *Server) panel(w http.ResponseWriter, r *http.Request) {
	if s.PanelPage == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// 面板 URL 含 token：不得落缓存；CSP 只放行内联样式/脚本与自身回路 fetch，
	// 禁止外部资源/表单提交/被嵌入（页面是本地静态料，无用户 HTML 注入）。
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
			"connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; "+
			"frame-ancestors 'none'")
	_, _ = w.Write([]byte(s.PanelPage))
}

// flux GET /flux：查流量（快照式，Python kernel_api.fetch_flux 同语义）。
// 缝未接线 → 501；查询失败 → 502 原样带回错误文本（不造假数据）。
func (s *Server) flux(w http.ResponseWriter, r *http.Request) {
	if s.FetchFlux == nil {
		http.Error(w, "流量查询缝未接线", http.StatusNotImplemented)
		return
	}
	data, err := s.FetchFlux()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

// config GET/POST /config：面板设置页（账号/密码/服务器/免认证时段）。
// GET 返回非敏感视图（密码绝不回传——凭据管理器是唯一的密码读写点）；
// POST 校验账号密码成对，落凭据 + 改 ini。缝未接线 → 501。
func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	if s.GetConfig == nil || s.SaveConfig == nil {
		http.Error(w, "配置缝未接线", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(s.GetConfig())
	case http.MethodPost:
		var in map[string]string
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&in); err != nil {
			http.Error(w, "JSON 解析失败", http.StatusBadRequest)
			return
		}
		user, hasUser := in["username"]
		pass, hasPass := in["password"]
		if hasPass != hasUser { // 只允许成对或都无（空 username 单独出现会清空 ini 账号）
			http.Error(w, "账号与密码必须成对提供", http.StatusBadRequest)
			return
		}
		// 业务校验先于任何写入：非法免认证时段不得先落凭据再拒（半写）。
		// 凭据不是配置：过滤 password 再交给通用校验钩子（防未来校验器日志泄漏）。
		if s.ValidateConfig != nil {
			view := make(map[string]string, len(in))
			for k, v := range in {
				if k != "password" {
					view[k] = v
				}
			}
			if err := s.ValidateConfig(view); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if hasPass {
			if user == "" {
				http.Error(w, "账号不能为空", http.StatusBadRequest)
				return
			}
			if pass == "" {
				// 空密码不得覆盖真凭据（2026-11-08 发布审查 P0：WriteCredential
				// 空 blob 会 panic，且写空密码等于毁掉已存密码）。
				http.Error(w, "密码不能为空（留空=不修改，请在面板留空）", http.StatusBadRequest)
				return
			}
			if s.SaveCredentials != nil {
				if err := s.SaveCredentials(user, pass); err != nil {
					http.Error(w, "凭据保存失败: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
			delete(in, "password") // ini 永不落密码
		}
		if err := s.SaveConfig(in); err != nil {
			http.Error(w, "配置保存失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// logs GET /logs：日志尾部行数组（新的在最后）。缝未接线 → 501；
// 读取失败 → 502 透传原因（不假装空日志，用户会误以为没记录）。
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	if s.Logs == nil {
		http.Error(w, "日志缝未接线", http.StatusNotImplemented)
		return
	}
	lines, err := s.Logs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(lines)
}

// rebindv6 POST /rebindv6：v6 补绑（门户观察 isOnline=false 时用当前 v6 源
// 地址主动登录一次，带 ipv4or6 字段）。缝未接线 → 501；失败 → 502 透传原文。
func (s *Server) rebindv6(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RebindV6 == nil {
		http.Error(w, "v6 补绑缝未接线", http.StatusNotImplemented)
		return
	}
	if err := s.RebindV6(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// accounts GET/POST /accounts：账号历史。GET 列最近使用（current 标记）；
// POST {username} 切换。缝未接线 → 501；切换失败 → 502 透传原因。
func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	if s.Accounts == nil {
		http.Error(w, "账号历史缝未接线", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(s.Accounts())
	case http.MethodPost:
		if s.SwitchAccount == nil {
			http.Error(w, "切换缝未接线", http.StatusNotImplemented)
			return
		}
		var in struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			http.Error(w, "JSON 解析失败", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(in.Username) == "" {
			http.Error(w, "username 不能为空", http.StatusBadRequest)
			return
		}
		msg, err := s.SwitchAccount(in.Username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Message string `json:"message"`
		}{Message: msg})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
