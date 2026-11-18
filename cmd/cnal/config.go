package main

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

// config 只承载本切片所需字段；凭据绝不进配置文件（环境变量/keyring 才是凭据的家）。
type config struct {
	Server    string
	LoginPath string
	LoginPage string // 门户页路径（Referer 用；空 = 只用 server）
	UserAgent string // 登录 UA（校事实；空 = protocol.DefaultUserAgent）
	PeerPath  string // 对端握手端点路径（校事实，来自配置；缺省根路径）
	PeerOpr   string // 对端握手操作名（词表，校事实；不设默认，异校必配）
	DualStack bool
	V6Peer    string
	CampusDNS string // 校园 DNS（选源探测地址，校事实；与守护同一选源规则）
	BindIP    string // 物理源逃生口（空 = 自动发现）
	// 首跑探测锚点（cnal detect 吃 ini；缺省由 app 通用默认补齐）。
	ProbeIPs  []string
	ProbePath string
	ProbeHost string
	// LoginPageMarks 门户登录页指纹（cnal detect 复核用；空 = app 默认
	// ac_portal/pwdLogin。异校必配：JS 渲染的门户页正文不含这两串）。
	LoginPageMarks []string
	Timeout        time.Duration
	SSLVerify      bool
	// Response [response] 词表（与守护同一套判定；零值字段回落默认）。
	Response keeper.ResponseMarks
	// LoginForm 登录表单模板（与守护同一份 login_form）。
	LoginForm protocol.Form
}

// loadConfig 解析 [portal] 或 [keeper] 节的 key = value（两节都吃：
// 正式守护 ini 用 [keeper]，CLI 老格式用 [portal]），忽略注释与未知键。
// server 与 login_path 都必填：端点路径是校事实，必须来自配置，不许焊死。
func loadConfig(path string) (config, error) {
	f, err := os.Open(path)
	if err != nil {
		return config{}, err
	}
	defer f.Close()

	var cfg config
	cfg.SSLVerify = true // 安全默认：缺省校验证书，显式 ssl_verify=false 才放开
	cfg.Response = keeper.DefaultResponseMarks()
	cfg.LoginForm = protocol.DefaultForm()
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			// 三节都吃：[portal]（CLI 老格式）、[keeper]（正式守护 ini）、
			// [response]（异校词表）。2026-11-08 发布审查 P0：只认 [portal]
			// 让 CLI 用正式配置直接缺 server。
			section = strings.TrimSpace(strings.Trim(line, "[]"))
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
				cfg.Response.Success = keeper.SplitMarks(v)
			case "negative_marks":
				cfg.Response.Negative = keeper.SplitMarks(v)
			case "transient_marks":
				cfg.Response.Transient = keeper.SplitMarks(v)
			case "freeze_marks":
				cfg.Response.Freeze = keeper.SplitMarks(v)
			}
			continue
		}
		if section != "portal" && section != "keeper" {
			continue
		}
		switch k {
		case "server":
			cfg.Server = v
		case "login_path":
			cfg.LoginPath = v
		case "login_form":
			f, parseErr := protocol.ParseForm(v)
			if parseErr != nil {
				return config{}, fmt.Errorf("login_form: %w", parseErr)
			}
			cfg.LoginForm = f
		case "login_page":
			cfg.LoginPage = v
		case "user_agent":
			cfg.UserAgent = v
		case "peer_path":
			cfg.PeerPath = v
		case "peer_opr":
			cfg.PeerOpr = v
		case "dual_stack":
			cfg.DualStack = v == "true" || v == "1" || v == "on"
		case "v6_peer":
			cfg.V6Peer = v
		case "campus_dns":
			cfg.CampusDNS = v
		case "bind_ip":
			cfg.BindIP = v
		case "probe_ips":
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					cfg.ProbeIPs = append(cfg.ProbeIPs, s)
				}
			}
		case "probe_path":
			cfg.ProbePath = v
		case "probe_host":
			cfg.ProbeHost = v
		case "login_page_marks":
			cfg.LoginPageMarks = keeper.SplitMarks(v)
		case "ssl_verify":
			cfg.SSLVerify = !(v == "false" || v == "0" || v == "off")
		case "timeout_sec":
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return config{}, fmt.Errorf("timeout_sec 不是整数: %w", convErr)
			}
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}
	if cfg.Server == "" {
		return config{}, errors.New("缺少 server")
	}
	if cfg.LoginPath == "" {
		return config{}, errors.New("缺少 login_path")
	}
	if cfg.PeerPath == "" {
		cfg.PeerPath = "/" // 缺省根路径；对端不在根时配置 peer_path 指到真实端点
	}
	if cfg.Timeout <= 0 {
		// 0 会让 net.Dialer/http.Client 无超时永久挂起（2026-11-08 发布审查 P0）。
		// 与守护 login_timeout 自适应同源：至少 6s。
		cfg.Timeout = 6 * time.Second
	}
	if cfg.UserAgent == "" {
		// 与守护同源：唯一出处在 protocol.DefaultUserAgent。
		cfg.UserAgent = protocol.DefaultUserAgent
	}
	return cfg, sc.Err()
}
