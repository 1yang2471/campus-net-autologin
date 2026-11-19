// Package netstack 网络客户端：两条选路通道。
//   - 物理栈（New）：所有请求必须绑定源地址发出。代理/加速器（FlClash 等）的
//     TUN 会接管默认路由与 DNS，不绑源的探测读到的是隧道出口（2026-11-06 实测：
//     v6 不绑源选出 fdfe:: 隧道源）。
//   - 系统栈（NewSystem）：不绑源，按系统默认路由/代理规则拨号，与浏览器同路。
//     仅用于 TUN 接管下需要域名解析的门户操作（设计 §3 路径 B）。
package netstack

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	mu       sync.RWMutex
	src      net.IP
	system   bool // 不绑源：走系统默认路由（构造后不变，无需加锁）
	perTry   time.Duration
	insecure bool
}

func New(src net.IP, timeout time.Duration) *Client {
	return &Client{src: src, perTry: timeout}
}

// NewSystem 系统栈客户端：不绑源、不钉 IP，域名交给系统/代理解析（TUN 下与
// 浏览器同路）。与物理栈的 pin 参数互斥——钉 IP 方法在系统栈上直接报错，
// 防止把绑源 DNS 的产物带错路。
func NewSystem(timeout time.Duration) *Client {
	return &Client{system: true, perTry: timeout}
}

// SetSource 在运行时改绑物理源（网络切换/休眠唤醒后旧源失效，组合根重发现
// 新源后调用）。加锁：手动重连在电路 API 的 goroutine 上，与 tick 并发送出。
func (c *Client) SetSource(ip net.IP) {
	c.mu.Lock()
	c.src = ip
	c.mu.Unlock()
}

// source 读当前源（每请求取一次，不缓存到局部变量之外）。
func (c *Client) source() net.IP {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.src
}

// Source 导出的当前绑定源（面板 IPv4 行显示实际物理源——TUN/代理环境下
// 「从哪张网卡出去」是用户最该看到的事实；重发现改绑后读数同步变）。
func (c *Client) Source() net.IP { return c.source() }

// InsecureTLS 显式跳过证书校验，仅用于用户配置 ssl_verify=false 的自签门户
// （校事实）；默认必须校验——安全默认由 TestTLSVerifyDefaultSecureOptOutExplicit 守着。
// 注意：跳过的是 **CA 链**，门户主机名仍然强制校验（见 tlsConfig）。
func (c *Client) InsecureTLS() *Client {
	c.insecure = true
	return c
}

// tlsConfig 本次请求的 TLS 配置。默认（ssl_verify=true）返回 nil → 走 Go 标准
// 校验（CA 链 + 主机名，SNI 取 URL 里的域名）。
//
// 用户显式 ssl_verify=false（自签门户）时：跳过 CA 链，但**仍校验对端证书的
// 门户主机名**。
//
// 为什么必须留这一关（2026-11-11 真机修正）：门户的公开 A 记录就是 1.1.1.2，
// 校外时 1.1.1.2 是真的 Cloudflare——完全不校验就等于把学号密码发给 Cloudflare。
// 冒名者拿不到门户域名的证书（私钥在门户手里），主机名一比就露馅。
// URL 主机是 IP 字面量时无从比对（证书得有对应 IP SAN），退回只跳链校验。
func (c *Client) tlsConfig(rawURL string) *tls.Config {
	if !c.insecure {
		return nil
	}
	cfg := &tls.Config{InsecureSkipVerify: c.insecure} //nolint:gosec // 用户显式配置 ssl_verify=false；门户主机名另有关验
	host := urlHostname(rawURL)
	if host != "" && net.ParseIP(host) == nil {
		cfg.VerifyPeerCertificate = hostOnlyVerifier(host)
	}
	return cfg
}

// hostOnlyVerifier 只比对证书主机名（不做链校验）：自签门户也能过，冒名证书过不了。
func hostOnlyVerifier(host string) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("对端未提供证书")
		}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("对端证书无法解析: %w", err)
		}
		if err := cert.VerifyHostname(host); err != nil {
			return fmt.Errorf("对端证书与门户主机名不符（%s）: %w", host, err)
		}
		return nil
	}
}

// urlHostname 取 URL 的主机名（去端口）；解析失败返回空串。
func urlHostname(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Get 走绑源连接。ipPin 非空时直连该 IP（host:port），Host 头与 TLS SNI
// 仍是 URL 里的域名——校园 DNS 绑源解析出的真实 IP 用这里（TUN 下域名
// 不可解析 / 会给 fake-ip）。接线与测试见 TestGetPinsIPKeepsHost。
func (c *Client) Get(rawURL string, headers map[string]string, ipPin string) (string, error) {
	return c.do(http.MethodGet, rawURL, nil, headers, ipPin)
}

// GetStatus 返回首响应状态码且不跟随跳转（守护 L1 探锚判据：3xx=劫持跳转、
// 200+指纹=劫持页、204=真在线——跟随会把劫持 302 读成 200，Python 守护同语义）。
func (c *Client) GetStatus(rawURL string, headers map[string]string) (int, string, error) {
	st, _, body, err := c.doStatusFull(http.MethodGet, rawURL, headers, "")
	return st, body, err
}

// GetStatusPinned 同 GetStatus，但实际连 ipPin（host:port，来自绑源 DNS 解析）。
func (c *Client) GetStatusPinned(rawURL string, headers map[string]string, ipPin string) (int, string, error) {
	st, _, body, err := c.doStatusFull(http.MethodGet, rawURL, headers, ipPin)
	return st, body, err
}

// GetStatusHeaders 首响应状态码 + 响应头 + 响应体（首跑门户探测用：未认证时
// 公网请求被 302 劫持，Location 自报门户地址——跟随跳转会丢掉这个事实）。
func (c *Client) GetStatusHeaders(rawURL string, headers map[string]string) (int, http.Header, string, error) {
	return c.doStatusFull(http.MethodGet, rawURL, headers, "")
}

// GetStatusPinnedHeaders 同 GetStatusHeaders，但实际连 ipPin（多网卡认证态
// 观察用：绑旁路源解析出真实 IP 后钉 IP 直连，TUN 下域名不可信）。
func (c *Client) GetStatusPinnedHeaders(rawURL string, headers map[string]string, ipPin string) (int, http.Header, string, error) {
	return c.doStatusFull(http.MethodGet, rawURL, headers, ipPin)
}

func (c *Client) doStatusPinned(method, rawURL string, headers map[string]string, ipPin string) (int, string, error) {
	st, _, body, err := c.doStatusFull(method, rawURL, headers, ipPin)
	return st, body, err
}

// dialer 造拨号器：物理栈要求非空源（fail-closed）；系统栈不设 LocalAddr，
// 按系统路由/代理规则拨号。
func (c *Client) dialer() (*net.Dialer, error) {
	if c.system {
		return &net.Dialer{Timeout: c.perTry}, nil
	}
	src := c.source()
	if src == nil {
		return nil, errors.New("未绑定源地址：物理栈纪律不允许")
	}
	return &net.Dialer{LocalAddr: &net.TCPAddr{IP: src}, Timeout: c.perTry}, nil
}

func (c *Client) doStatusFull(method, rawURL string, headers map[string]string, ipPin string) (int, http.Header, string, error) {
	// CRLF 防线先行：头值带换行直接拒绝，不触网。
	for k, v := range headers {
		if strings.ContainsAny(v, "\r\n") {
			return 0, nil, "", fmt.Errorf("拒绝含换行的请求头: %s", k)
		}
	}
	if ipPin != "" && c.system {
		return 0, nil, "", errors.New("系统栈客户端不支持钉 IP（pin 只属于绑源路径）")
	}
	d, err := c.dialer()
	if err != nil {
		return 0, nil, "", err
	}
	tr := &http.Transport{
		DialContext:         pinnedDialer(d, rawURL, ipPin),
		DisableKeepAlives:   true,
		MaxIdleConnsPerHost: -1,
	}
	if cfg := c.tlsConfig(rawURL); cfg != nil {
		// 用户显式配置 ssl_verify=false（自签门户）：跳过 CA 链，但**仍校验
		// 对端证书的门户主机名**——冒名者拿不到门户域名的证书。
		tr.TLSClientConfig = cfg
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{
		Transport: tr,
		Timeout:   c.perTry,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 首响应状态直达调用方
		},
	}
	req, err := http.NewRequestWithContext(context.Background(), method, rawURL, nil)
	if err != nil {
		return 0, nil, "", err
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v // Go 客户端 Host 必须走 req.Host，Header.Set 对 Host 无效
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return 0, nil, "", err
	}
	return resp.StatusCode, resp.Header, string(b), nil
}

func (c *Client) Post(url string, form map[string]string, headers map[string]string) (string, error) {
	return c.do(http.MethodPost, url, form, headers, "")
}

// PostPinned 同 Post，但实际连 ipPin（host:port，来自绑源 DNS 解析）。
func (c *Client) PostPinned(rawURL string, form map[string]string, headers map[string]string, ipPin string) (string, error) {
	return c.do(http.MethodPost, rawURL, form, headers, ipPin)
}

// GetPinned 同 Get，但实际连 ipPin（host:port）。GET 型登录端点（Dr.COM/Srun
// 明文族）的参数走 query——与 PostPinned 共用同一条绑源/钉 IP/Host 头实现，
// 只在 do() 里按 method 决定 form 进 query 还是 body（HTTP 方法只改请求行，
// urlencoded 编码规则两族相同）。
func (c *Client) GetPinned(rawURL string, form map[string]string, headers map[string]string, ipPin string) (string, error) {
	return c.do(http.MethodGet, rawURL, form, headers, ipPin)
}

func (c *Client) do(method, rawURL string, form map[string]string, headers map[string]string, ipPin string) (string, error) {
	// CRLF 防线先行：头值带换行直接拒绝，不触网。
	for k, v := range headers {
		if strings.ContainsAny(v, "\r\n") {
			return "", fmt.Errorf("拒绝含换行的请求头: %s", k)
		}
	}
	if ipPin != "" && c.system {
		return "", errors.New("系统栈客户端不支持钉 IP（pin 只属于绑源路径）")
	}
	d, err := c.dialer()
	if err != nil {
		return "", err
	}
	tr := &http.Transport{
		DialContext:         pinnedDialer(d, rawURL, ipPin),
		DisableKeepAlives:   true,
		MaxIdleConnsPerHost: -1,
	}
	if cfg := c.tlsConfig(rawURL); cfg != nil {
		// 用户显式配置 ssl_verify=false（校事实，守护同款配置面）；默认
		// insecure=false 即正常校验，TestTLSVerifyDefaultSecureOptOutExplicit 两头守着
		tr.TLSClientConfig = cfg
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{Transport: tr, Timeout: c.perTry}

	// GET 型登录（Dr.COM/Srun 明文族）：form 进 query，body 必须为空、
	// 不设 form Content-Type——与 POST 共用编码规则，只换放置位置。
	if method == http.MethodGet && len(form) > 0 {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", err
		}
		q := u.Query()
		for k, v := range form {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}
	var body io.Reader
	contentType := ""
	if method != http.MethodGet && len(form) > 0 {
		vals := url.Values{}
		for k, v := range form {
			vals.Set(k, v)
		}
		body = strings.NewReader(vals.Encode())
		contentType = "application/x-www-form-urlencoded"
	}
	req, err := http.NewRequestWithContext(context.Background(), method, rawURL, body)
	if err != nil {
		return "", err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v // Go 客户端 Host 必须走 req.Host，Header.Set 对 Host 无效
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	// do 承载登录与 flux 查询：userflux HTML 表可能多字段多行，2048 会截断
	// 到半个 <tr> → parseFluxTable 空表 → 会话误判死触发无谓重连
	// （2026-11-08 发布审查 P1）。L1 探锚走 doStatus，仍保持 2048。
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// pinnedDialer 钉 IP 拨号：ipPin 非空时实际连接目标是 pin 的 host:port，
// Host 头与 TLS SNI 仍是 URL 里的域名。ipPin 必须来自绑源 DNS 解析结果，
// 不是用户输入拼接（否则“直连企业内网任意地址”成凭据外泄口）。
func pinnedDialer(d *net.Dialer, rawURL, ipPin string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	// 解析 URL host 只用于“非钉 IP”时的默认目标与错误诊断；钉 IP 时忽略。
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		target := ipPin
		if target == "" {
			u, err := url.Parse(rawURL)
			if err != nil {
				return nil, err
			}
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "80"
				if u.Scheme == "https" {
					port = "443"
				}
			}
			target = net.JoinHostPort(host, port)
		}
		return d.DialContext(ctx, network, target)
	}
}
