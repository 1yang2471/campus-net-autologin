package app

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"cnal/internal/netstack"
	"cnal/internal/protocol"
)

// 首跑探测的通用缺省值（不绑定任何学校；Python detect_portal/DEFAULTS 同值）。
// loadDaemonConfig 的缺省也引用这里——同一规矩只放一处。
const (
	defaultProbePath = "/generate_204"
	defaultProbeHost = "connect.rom.miui.com"
)

func defaultProbeIPs() []string { return []string{"223.5.5.5", "119.29.29.29"} }

// DetectOptions 首跑门户探测输入；零值 = 通用默认。
type DetectOptions struct {
	CampusDNS string        // 空 = 系统 DNS 回退（TUN 下可能给 fake-ip）
	BindIP    string        // 物理源逃生口（空 = 自动发现）
	ProbeIPs  []string      // 公网锚点（空 = 223.5.5.5,119.29.29.29）
	ProbePath string        // 锚点路径（空 = /generate_204）
	ProbeHost string        // 锚点 Host 头（空 = connect.rom.miui.com）
	LoginPage string        // 门户登录页（空 = ac_portal 族默认）
	// LoginPageMarks 门户登录页指纹（空 = ac_portal/pwdLogin）。异校必须可配：
	// 真机实测本校登录页正文不含这两串（表单 JS 渲染），默认指纹恒失败。
	LoginPageMarks []string
	Timeout        time.Duration // 单请求超时（空 = 4s）
}

// defaultLoginPageMarks 登录页指纹缺省值（唯一出处：DefaultFormTemplate 同族，
// 异校用 ini 的 login_page_marks 覆盖）。
func defaultLoginPageMarks() []string { return []string{"ac_portal", "pwdLogin"} }

// DetectPortal 校园网免填探测（Python detect_portal 同语义）：未认证时公网
// 请求被门户 302 劫持，Location 自报门户地址。两道防线后才返回：
//  1. parsePortalLocation 拒绝非 http/https、环回、localhost、链路本地；
//  2. GET 门户登录页验证 ac_portal/pwdLogin 指纹——钓鱼/路由器劫持页过不了
//     这关，宁缺毋滥返回 ""（已登录/不在校园网时同样返回错误）。
//
// 全程无凭据 GET：探测链路绝不发送学号密码。
func DetectPortal(opt DetectOptions) (string, error) {
	if opt.Timeout <= 0 {
		opt.Timeout = 4 * time.Second
	}
	if opt.ProbePath == "" {
		opt.ProbePath = defaultProbePath
	}
	if opt.ProbeHost == "" {
		opt.ProbeHost = defaultProbeHost
	}
	if opt.LoginPage == "" {
		opt.LoginPage = protocol.DefaultLoginPage
	}
	probeIPs := opt.ProbeIPs
	if len(probeIPs) == 0 {
		probeIPs = defaultProbeIPs()
	}
	src := detectionSource(opt)
	if src == nil {
		return "", errors.New("无可用物理 v4 源（TUN 占满？）——物理栈纪律下不触网")
	}
	client := netstack.New(src, opt.Timeout)
	loginPath := strings.Split(opt.LoginPage, "?")[0]
	var diag []string // 每步的实测结论：首跑向导失败时，用户要看得见卡在哪一环
	for _, probeIP := range probeIPs {
		for _, host := range probeHostVariants(opt.ProbeHost) {
			hdrs := map[string]string{}
			label := probeIP
			if host != "" {
				hdrs["Host"] = host
				label = probeIP + " Host=" + host
			}
			st, hdr, body, err := client.GetStatusHeaders("http://"+probeIP+opt.ProbePath, hdrs)
			if err != nil {
				diag = append(diag, label+": 不可达")
				continue // 0 = 不可达
			}
			if isRedirect(st) {
				loc := hdr.Get("Location")
				base, ok := parsePortalLocation(loc)
				if !ok {
					diag = append(diag, fmt.Sprintf("%s: %d 但 Location 不可用(%s)", label, st, loc))
					continue
				}
				if verifyPortalLogin(base, loginPath, src, opt) {
					return base, nil
				}
				diag = append(diag, fmt.Sprintf("%s: %d→门户 %s，但登录页指纹复核未过（比对指纹：%s）",
					label, st, base, strings.Join(effectiveMarks(opt), "/")))
				continue
			}
			if st == 200 {
				// 200 型劫持页（部分门户不 302，直接回登录页）：只从正文里含
				// ac_portal 的绝对 URL 取门户基址——相对链接会落到锚点 IP 上，
				// 不可信；取到后仍要指纹复核。204/404 = 已认证真实到达公网。
				if base, ok := portalBaseFromBody(body); ok && verifyPortalLogin(base, loginPath, src, opt) {
					return base, nil
				}
				diag = append(diag, fmt.Sprintf("%s: 200（未认证时也返回真页面，疑被放行）", label))
				continue
			}
			diag = append(diag, fmt.Sprintf("%s: %d（已认证的正常应答）", label, st))
		}
	}
	return "", fmt.Errorf("未探测到校园门户。逐锚点实测：%s", strings.Join(diag, "；"))
}

// probeHostVariants 一次探测要试的 Host 头取值：**先不带 Host**，再补配置的那个。
//
// 为什么要试两种（2026-11-11 真机实证，同一未认证态下对照）：
//
//	http://223.5.5.5/generate_204           → 302 门户（劫持）
//	同 URL + Host: connect.rom.miui.com    → 404（**被白名单放行**）
//	同 URL + Host: www.example.com         → 302 门户（劫持）
//
// 校园把小米/微软的强制门户探测域名放行了（msftconnecttest 同样返回真 200），
// 于是用这些域名当 Host 的探测**在未认证时也拿到真实应答**、永远看不到劫持。
// 不带 Host（Host = 目标 IP）绕开按域名放行的白名单；但有些校园反过来按域名
// 劫持（DNS 劫持 + 按 Host 返门户页），所以配置的 Host 仍保留为第二次尝试。
func probeHostVariants(configured string) []string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return []string{""}
	}
	return []string{"", configured}
}

// effectiveMarks 实际生效的登录页指纹（空 = 默认档），供诊断文本用。
func effectiveMarks(opt DetectOptions) []string {
	if len(opt.LoginPageMarks) == 0 {
		return defaultLoginPageMarks()
	}
	return opt.LoginPageMarks
}

// detectionSource 选物理源：bind_ip 手工钉源优先，否则 UDP connect 选路。
func detectionSource(opt DetectOptions) net.IP {
	if opt.BindIP != "" {
		ip := net.ParseIP(opt.BindIP)
		if ip == nil || !netstack.UsableSource(ip) {
			return nil
		}
		return ip
	}
	return netstack.DiscoverSource(opt.CampusDNS)
}

// verifyPortalLogin 拉取门户登录页做指纹验证（自签门户常见，无凭据 GET
// 才允许跳过证书校验；返回 true 才认为探测成功）。
//
// 指纹取自 opt.LoginPageMarks，缺省 ac_portal/pwdLogin。**必须可配**：真机实测
// 本校登录页正文不含这两串（表单由 JS 渲染），只有 <title>上网认证系统</title>
// 与 password_name 稳定存在——硬编码的指纹在异校恒失败（2026-11-11）。
func verifyPortalLogin(base, loginPath string, src net.IP, opt DetectOptions) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := u.Hostname()
	pip, err := portalIPFor(host, src, opt)
	if err != nil {
		return false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	client := netstack.New(src, opt.Timeout).InsecureTLS()
	st, body, err := client.GetStatusPinned(base+loginPath, nil,
		net.JoinHostPort(pip.String(), port))
	if err != nil || st == 0 {
		return false
	}
	marks := opt.LoginPageMarks
	if len(marks) == 0 {
		marks = defaultLoginPageMarks()
	}
	return portalBodyMatchesMarks(body, marks)
}

// portalBodyMatchesMarks 正文是否命中任一登录页指纹（大小写不敏感；空白项忽略）。
// 抽成纯函数是为了钉住「本校页面在默认指纹下判负、在配置指纹下判正」这条
// 实测事实——它决定了首跑向导走不走得通（2026-11-11）。
func portalBodyMatchesMarks(body string, marks []string) bool {
	low := strings.ToLower(body)
	for _, m := range marks {
		if m = strings.ToLower(strings.TrimSpace(m)); m != "" && strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// portalIPFor 门户 host → 真实 IPv4：配置了 campus_dns 就绑源解析（TUN 下
// 系统 DNS 会给 fake-ip），否则回退系统 DNS。
func portalIPFor(host string, src net.IP, opt DetectOptions) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, errors.New("门户地址不是 IPv4")
		}
		return ip, nil
	}
	if strings.TrimSpace(opt.CampusDNS) != "" {
		if ip, err := netstack.ResolveBound(src, opt.CampusDNS, host, opt.Timeout); err == nil {
			return ip, nil
		}
		// 校园 DNS 暂时不可达时回退系统 DNS（Python detect_portal 同序：
		// campus_dns_resolve(...) or system_dns_resolve(...)）；ResolveSystem
		// 过滤 fake-ip，不会钉到 TUN 隧道地址。
	}
	return netstack.ResolveSystem(host)
}

// parsePortalLocation 从未认证时门户劫持页的 302 Location 提取门户基址
// （scheme+host[:port]）。安全边界（Python parse_portal_location 同源）：
// 仅接受 http/https；拒绝环回/localhost/链路本地/未指定/组播——这些永远
// 不是校园门户。校园门户住在私有段属产品目标，不拒绝；此处不发凭据。
func parsePortalLocation(loc string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(loc))
	if err != nil || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := u.Hostname()
	low := strings.ToLower(host)
	if low == "localhost" || strings.HasSuffix(low, ".localhost") {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			return "", false
		}
	}
	if strings.Contains(host, ":") { // IPv6 字面量 URL 需要方括号
		host = "[" + host + "]"
	}
	port := ""
	if p := u.Port(); p != "" {
		port = ":" + p
	}
	return scheme + "://" + host + port, true
}

// reAbsURL 正文里的绝对 URL（200 型劫持页兜底用）。只接受明确含 ac_portal
// 的链接：门户地址必须自报，探测器不靠猜。
var reAbsURL = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+`)

// portalBaseFromBody 从 200 劫持页正文提取门户基址；没有 ac_portal 指纹
// 或只有相对链接时返回 false（相对链接会落到锚点 IP 上，不可信）。
func portalBaseFromBody(body string) (string, bool) {
	if !strings.Contains(strings.ToLower(body), "ac_portal") {
		return "", false
	}
	for _, raw := range reAbsURL.FindAllString(body, -1) {
		if !strings.Contains(strings.ToLower(raw), "ac_portal") {
			continue
		}
		if base, ok := parsePortalLocation(raw); ok {
			return base, true
		}
	}
	return "", false
}

func isRedirect(st int) bool {
	switch st {
	case 301, 302, 307, 308:
		return true
	}
	return false
}
