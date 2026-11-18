package main

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"cnal/internal/dialect"
	"cnal/internal/keeper"
	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

const (
	exitOK       = 0 // 完成（补绑成功/对端本就在线/纯 v4 登录成功）
	exitCfg      = 2 // 配置错误
	exitS1       = 3 // S1 不可观测 → 未发登录
	exitNoCred   = 4 // 缺凭据环境变量
	exitCircuit  = 5 // 速率闸门关闭
	exitNoDaemon = 7 // 守护未运行/未响应（status 专用）
)

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

// dispatch 子命令路由（独立函数供测试；main 只做退出码映射）。
func dispatch(args []string) int {
	if len(args) < 1 {
		usage()
		return exitCfg
	}
	switch args[0] {
	case "rebind":
		return runRebindCmd(args[1:])
	case "detect", "--detect": // --detect 是 README/对比文档里的兼容写法
		return runDetectCmd(args[1:])
	case "compat":
		return runCompatCmd(args[1:])
	case "status", "--status": // 只读查运行中守护的状态
		return runStatusCmd(args[1:])
	default:
		usage()
		return exitCfg
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法:\n"+
		"  cnal status [--runtime <api.json>]\n"+
		"  cnal rebind --config <ini>\n"+
		"  cnal detect [--config <ini>] [--write] [--campus-dns <ip>] [--bind-ip <ip>]\n"+
		"  cnal compat \"<响应样本>\" | cnal compat --detect --file <登录页样本>")
}

func runRebindCmd(args []string) int {
	fs := flag.NewFlagSet("rebind", flag.ExitOnError)
	cfgPath := fs.String("config", "", "ini 配置文件路径（[keeper]/[portal] 节）")
	fs.Parse(args)
	if *cfgPath == "" {
		fs.Usage()
		return exitCfg
	}
	return run(*cfgPath)
}

func run(cfgPath string) int {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "配置错误:", err)
		return exitCfg
	}
	// 物理源与守护同源：DiscoverSource（UDP connect 校园 DNS 取出口源，
	// 绕开 TUN）+ PhysicalSourceV6。选源规则全局唯一，rebind 不再踩 TUN 病。
	v4Src := netstack.DiscoverSource(cfg.CampusDNS)
	v6Src := netstack.PhysicalSourceV6()
	if v4Src == nil {
		fmt.Println("无可用物理 v4 源——物理栈纪律下不触网")
		return exitCfg
	}
	fmt.Printf("v4 源: %s\nv6 源: %s\n", srcStr(v4Src), srcStr(v6Src))

	dualStack := cfg.DualStack
	var peerVal string
	var peerErr error
	if dualStack {
		if v6Src == nil {
			fmt.Println("v6 状态: S0 无全局 v6 源 → 完全静默，不发登录")
			return exitS1
		}
		if cfg.V6Peer == "" {
			// 切片一不含门户 JS 提取（拼接/裸变量形态降级 S1，规格 §6.1）：
			// 未显式配置 v6_peer 即缺失，不得变成一次补绑。
			fmt.Println("v6 状态: S1 不可观测（未配置 v6_peer，对端提取在切片二）→ 不发登录")
			return exitS1
		}
		if cfg.PeerOpr == "" {
			fmt.Println("配置错误: dual_stack 开且配了 v6_peer 时必须配置 peer_opr（握手操作名是词表，异校必配）")
			return exitCfg
		}
		handshakePeer := protocol.NewPeer(peerURL(cfg.V6Peer)+cfg.PeerPath, cfg.Timeout)
		resp, qerr := handshakePeer.Query(v6Src, cfg.PeerOpr)
		if qerr != nil {
			fmt.Println("对端握手失败:", qerr)
			peerErr = protocol.ErrPeerQuery
		} else {
			state := protocol.InterpretHandshake(resp)
			fmt.Printf("对端握手: %v\n", state)
			switch state {
			case protocol.StatePeerOffline:
				m, _ := dialect.Parse(resp)
				peerVal = strVal(m["ip"])
				if peerVal == "" {
					fmt.Printf("对端未在线但应答缺 ip（前120字）: %q\n", trunc(resp, 120))
					peerErr = protocol.ErrPeerQuery
				}
			case protocol.StatePeerOnline:
				fmt.Println("对端已在线 → 无需补绑")
				return exitOK
			default:
				fmt.Printf("对端应答原文（前120字）: %q\n", trunc(resp, 120))
				peerErr = protocol.ErrPeerQuery
			}
		}
	}

	steps := PlanRebind(dualStack, peerVal, peerErr)
	if len(steps) == 0 {
		fmt.Println("S1 不可观测 → 不发登录（缺失/失败都不得变成补绑）")
		return exitS1
	}

	user, pass := os.Getenv("CNAL_USER"), os.Getenv("CNAL_PASS")
	if user == "" || pass == "" {
		fmt.Println("需要凭据：设置 CNAL_USER / CNAL_PASS 环境变量后重试（本工具不落盘、不写日志）")
		return exitNoCred
	}
	rl := keeper.NewRateLimit(8, time.Hour, 15*time.Minute, time.Now)
	if !rl.AllowAutoLogin() {
		fmt.Println("速率闸门：滚动 1 小时内自动登录已达上限 → 拒绝（手动登录随时可越权）")
		return exitCircuit
	}

	settings := protocol.Settings{DualStack: dualStack, Form: cfg.LoginForm}
	provider := protocol.NewPeerProvider(func() (string, error) { return "", protocol.ErrPeerAbsent })
	if dualStack {
		provider = protocol.NewPeerProvider(func() (string, error) { return peerVal, nil })
	}
	form := protocol.BuildLogin(user, pass, settings, provider)
	client := netstack.New(v4Src, cfg.Timeout)
	if !cfg.SSLVerify {
		client = client.InsecureTLS() // 用户显式配置的自签门户（守护同款 ssl_verify 配置面）
	}
	// 门户域名在 TUN 下会被系统 DNS 解析成 fake-ip（2026-11-08 真机实证）：
	// 必须绑校园 DNS 解析出真实 IP 再 PostPinned，否则凭据发往 28.x 超时。
	u, err := url.Parse(cfg.Server)
	if err != nil {
		fmt.Println("配置错误: server 非法 URL:", err)
		return exitCfg
	}
	portalIP, err := netstack.ResolveBound(v4Src, cfg.CampusDNS, u.Hostname(), cfg.Timeout)
	if err != nil || portalIP == nil {
		fmt.Println("门户解析失败（疑似不在校园网）——不把凭据发往猜测地址:", err)
		return exitCfg
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	// UA/Referer 是门户 WAF 校事实（与守护 deliver 同源，AGENTS.md 第 10 条）。
	// UA 与 login_page 都从 ini 读，缺省 UA 取 protocol.DefaultUserAgent；
	// Referer = server + login_page（login_page 空时即 server）。
	hdr := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"User-Agent":   cfg.UserAgent,
		"Referer":      cfg.Server + cfg.LoginPage,
	}
	resp, err := client.PostPinned(cfg.Server+cfg.LoginPath, form, hdr,
		net.JoinHostPort(portalIP.String(), port))
	if err != nil {
		fmt.Println("登录请求失败:", err)
		return exitS1
	}
	// 与守护同一套响应判定（JSON/伪 JSON/ret_code/纯文本 + [response] 词表）：
	// CLI 不能用比守护更窄的 success 布尔判定，否则异校在 CLI 上永远「登录未成功」。
	if _, jerr := keeper.JudgeLoginResponse(resp, cfg.Response); jerr == nil {
		rl.RecordSuccess(time.Now())
		fmt.Println("登录成功（v4）")
	} else {
		fmt.Printf("登录未成功: %s（判定: %v）\n", strings.TrimSpace(resp), jerr)
		return exitS1
	}

	if dualStack {
		recheckPeer := protocol.NewPeer(peerURL(cfg.V6Peer)+cfg.PeerPath, cfg.Timeout)
		resp2, qerr := recheckPeer.Query(v6Src, cfg.PeerOpr)
		if qerr != nil {
			fmt.Println("复查对端失败:", qerr)
			return exitS1
		}
		fmt.Printf("复查对端: %v\n", protocol.InterpretHandshake(resp2))
	}
	return exitOK
}

// peerURL 把裸 v6 字面量补成带 scheme 的完整 URL 前缀（路径由配置的
// peer_path 提供——端点路径是校事实，不进代码）。
func peerURL(host string) string {
	if strings.Contains(host, "://") {
		return host
	}
	return "http://[" + host + "]"
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func srcStr(ip net.IP) string {
	if ip == nil {
		return "（无）"
	}
	return ip.String()
}

func strVal(v any) string {
	s, _ := v.(string)
	return s
}
