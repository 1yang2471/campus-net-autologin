# Windows 接口级绑网 Implementation Plan — Tasks 3–6

### Task 4: 严格模式配置与凭据路径 fail-closed

**Files:**
- Modify: `go-cnal/internal/app/config.go:18-68,125-245,250-317`
- Modify: `go-cnal/internal/app/compose_portal.go:19-65,89-238,261-271`
- Modify: `go-cnal/internal/app/portalroute.go:45-104,122-135`
- Modify: `go-cnal/internal/app/channel_probe.go:62-76`
- Modify: `go-cnal/internal/app/channel_login.go:33-84`
- Test: `go-cnal/internal/app/config_test.go`
- Test: `go-cnal/internal/app/portalroute_test.go`
- Test: `go-cnal/internal/app/portal_route_bind_test.go`
- Test: `go-cnal/internal/app/portal_route_wiring_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestConfigStrictInterfaceBindDefaultAndOverride(t *testing.T) {
	base := "[keeper]\nserver = https://x\nlogin_path = /a\n"
	c, err := loadDaemonConfig(writeTmpIni(t, base))
	if err != nil || !c.StrictInterfaceBind {
		t.Fatalf("strict_interface_bind 缺省必须为 true，实际 %v %v", c.StrictInterfaceBind, err)
	}
	c, err = loadDaemonConfig(writeTmpIni(t, base+"strict_interface_bind = false\n"))
	if err != nil || c.StrictInterfaceBind {
		t.Fatalf("显式 false 应允许降级，实际 %v %v", c.StrictInterfaceBind, err)
	}
	if _, err := loadDaemonConfig(writeTmpIni(t, base+"strict_interface_bind = maybe\n")); err == nil {
		t.Fatal("非法布尔值必须报错")
	}
}

func TestRouteAutoStrictRefusesSystemFallback(t *testing.T) {
	p, _, system := routeFixture(NetModeAuto, func() (net.IP, error) {
		return nil, errors.New("DNS 不通")
	})
	p.strictInterfaceBind = true
	r := p.routeLogin()
	if r.client != nil || r.path != routeBound {
		t.Fatalf("严格 auto 必须 fail-closed，实际 client=%v path=%s", r.client, r.path)
	}
	_ = system
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go -C go-cnal test ./internal/app -run 'TestConfigStrictInterfaceBind|TestRouteAutoStrictRefusesSystemFallback' -count=1`

Expected: 编译失败，配置与 plumbing 字段不存在。

- [ ] **Step 3: 增加配置字段与解析**

`daemonConfig` 增加：

```go
StrictInterfaceBind bool
ClashAPIURL         string
ClashAPISecret      string
```

初始化与解析：

```go
c.StrictInterfaceBind = true

case "strict_interface_bind":
	b, err := parseConfigBool(k, v)
	if err != nil {
		return daemonConfig{}, err
	}
	c.StrictInterfaceBind = b
case "clash_api_url":
	c.ClashAPIURL = strings.TrimSpace(v)
case "clash_api_secret":
	c.ClashAPISecret = v
```

布尔解析：

```go
func parseConfigBool(key, v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on":
		return true, nil
	case "false", "0", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s 只能是 true/false，实际 %q", key, v)
	}
}
```

- [ ] **Step 4: 组合根使用同一绑定模式**

`plumbing` 增加：

```go
bindMode            netstack.BindMode
strictInterfaceBind bool
```

`newPlumbing` 在源确定后：

```go
bindMode := netstack.ModeForStrict(cfg.StrictInterfaceBind)
strict := cfg.StrictInterfaceBind && netstack.DefaultBindMode() == netstack.BindModeInterface
```

所有物理客户端改为 `netstack.NewWithBindMode(src, timeout, bindMode)`；门户解析改为 `ResolveBoundWithBindMode(..., bindMode)`。把包级探测缝改为 `var portalProbeWithBindMode = netstack.ProbePortalWithBindMode`，SourcePicker 闭包调用 `portalProbeWithBindMode(..., bindMode)`，测试替身同步替换该缝。`p` 字面量写入 `bindMode` 与 `strictInterfaceBind`。

`channelAuthState` 与 `channelLogin` 中的 `ResolveBound`、`netstack.New` 也分别改为 `ResolveBoundWithBindMode(..., p.bindMode)`、`NewWithBindMode(..., p.bindMode)`；首跑无凭据探测保留默认包装语义。

`applyV4` 保持调用 `SetSource`；接口索引每次拨号重新解析，不缓存。

- [ ] **Step 5: 修改选路策略与错误分类**

`chooseRoute` 在解析失败后：

```go
if errors.Is(err, netstack.ErrInterfaceBind) {
	err = errSourceUnbindable
}
```

fail-closed 条件：

```go
if mode == NetModeBound || (mode == NetModeAuto && p.strictInterfaceBind) {
	return p.noteRoute(portalRoute{
		path: routeBound,
		why:  failKind + "（" + routeErrText(err) + "），严格接口绑定拒绝转系统栈",
	})
}
```

`routeFixture` 显式设置 `strictInterfaceBind: false`，保留旧 auto 回退测试；新增严格测试覆盖手动登录和首跑确认的同一路径。

- [ ] **Step 6: 运行 app 全量测试并修正旧预期**

Run: `go -C go-cnal test ./internal/app -count=1`

Expected: PASS；旧测试仅在 fixture 中显式选择非严格，生产默认严格。

- [ ] **Step 7: 提交**

```bash
git add go-cnal/internal/app/config.go go-cnal/internal/app/compose_portal.go go-cnal/internal/app/portalroute.go go-cnal/internal/app/channel_probe.go go-cnal/internal/app/channel_login.go go-cnal/internal/app/config_test.go go-cnal/internal/app/portalroute_test.go go-cnal/internal/app/portal_route_bind_test.go go-cnal/internal/app/portal_route_wiring_test.go
git commit -m "fix(app): 严格接口绑定默认拒绝凭据回退系统栈"
```

---

### Task 5: 实现仅本机回环的 Clash fake-ip flush

**Files:**
- Create: `go-cnal/internal/clashapi/client.go`
- Test: `go-cnal/internal/clashapi/client_test.go`
- Modify: `go-cnal/internal/keeper/login.go:18-35,104-175`
- Test: `go-cnal/internal/keeper/login_test.go`
- Modify: `go-cnal/internal/app/compose_portal.go`
- Test: `go-cnal/internal/app/clash_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestFlushFakeIPUsesMihomoEndpointAndBearer(t *testing.T) {
	var method, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c, err := clashapi.New(srv.URL, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.FlushFakeIP(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/cache/fakeip/flush" || auth != "Bearer secret" {
		t.Fatalf("请求形状错误: %s %s %s", method, path, auth)
	}
}

func TestRejectsNonLoopbackController(t *testing.T) {
	for _, raw := range []string{"http://example.com:9090", "https://127.0.0.1:9090", "http://user@127.0.0.1:9090", "http://127.0.0.1:9090/?x=1"} {
		if _, err := clashapi.New(raw, ""); err == nil {
			t.Fatalf("应拒绝非安全控制器地址 %q", raw)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go -C go-cnal test ./internal/clashapi -count=1`

Expected: 包不存在。

- [ ] **Step 3: 实现客户端**

```go
package clashapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

func New(rawURL, secret string) (*Client, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("clash_api_url 仅允许无凭据、无查询串的本机回环 HTTP 地址")
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("clash_api_url 必须指向本机回环，实际 %q", host)
		}
	}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if strings.EqualFold(host, "localhost") {
				host = "127.0.0.1"
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	return &Client{
		baseURL: strings.TrimRight(u.String(), "/"),
		secret:  secret,
		http: &http.Client{
			Transport: tr,
			Timeout:   2 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) FlushFakeIP(ctx context.Context) error {
	if c == nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/cache/fakeip/flush", nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Clash fake-ip flush 返回 HTTP %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: 给 LoginAction 增加成功后回调**

```go
AfterSuccess func()
```

在 `DoVia`、`DoBind`、`DoRebind` 的判定成功后调用：

```go
if a.AfterSuccess != nil {
	a.AfterSuccess()
}
```

测试断言：成功一次回调一次；投递失败、门户判负、补绑失败均不回调。

- [ ] **Step 5: 接入组合根**

`plumbing` 增加 `clash *clashapi.Client`，并在 `p` 字面量中写入 `clash: clash`。

`newPlumbing` 解析配置：

```go
clash, err := clashapi.New(cfg.ClashAPIURL, cfg.ClashAPISecret)
if err != nil {
	return nil, fmt.Errorf("Clash API 配置错误: %w", err)
}
```

`p.action.AfterSuccess = p.flushClash`。

```go
func (p *plumbing) flushClash() {
	if p.clash == nil {
		return
	}
	if err := p.clash.FlushFakeIP(context.Background()); err != nil {
		msg := fmt.Sprintf("Clash fake-ip flush 失败（不影响认证）: %v", err)
		if p.logf != nil {
			p.logf("%s\n", msg)
		}
		_ = AppendEvent(LogFilePath(), EventClock(), msg)
	}
}
```

- [ ] **Step 6: 运行相关测试**

Run: `go -C go-cnal test ./internal/clashapi ./internal/keeper ./internal/app -count=1`

Expected: PASS。

- [ ] **Step 7: 提交**

```bash
git add go-cnal/internal/clashapi go-cnal/internal/keeper/login.go go-cnal/internal/keeper/login_test.go go-cnal/internal/app/compose_portal.go go-cnal/internal/app/clash_test.go go-cnal/go.mod go-cnal/go.sum
git commit -m "feat(proxy): 登录成功后刷新本机 Clash fake-ip 缓存"
```

---

### Task 6: 暴露绑定诊断并保持旧客户端兼容

**Files:**
- Modify: `go-cnal/internal/api/api.go:23-68`
- Modify: `go-cnal/internal/app/compose_api.go:30-64`
- Modify: `go-cnal/internal/app/portalroute.go:122-135`
- Test: `go-cnal/internal/api/api_test.go`
- Test: `go-cnal/internal/app/portalroute_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestRouteTextIncludesBindingMode(t *testing.T) {
	p, _, _ := routeFixture(NetModeAuto, func() (net.IP, error) {
		return net.IPv4(10, 129, 0, 9), nil
	})
	p.routePortal()
	if got := p.routeText(); !strings.Contains(got, "接口绑定") && !strings.Contains(got, "仅绑源") {
		t.Fatalf("路径文本应暴露绑定模式，实际 %q", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go -C go-cnal test ./internal/app -run TestRouteTextIncludesBindingMode -count=1`

Expected: FAIL。

- [ ] **Step 3: 增加 API 可选字段**

`api.Snapshot` 增加：

```go
BindMode      string `json:"bind_mode,omitempty"`
BindInterface string `json:"bind_interface,omitempty"`
BindIndex     int    `json:"bind_index,omitempty"`
BindDegraded  bool   `json:"bind_degraded,omitempty"`
BindError     string `json:"bind_error,omitempty"`
```

`compose_api.Snapshot` 从 `p.portal.BindingInfo()` 填充；`Err` 仅写错误文本，不写 secret。

`routeText` 在 `routeBound` 时追加（`portalroute.go` 增加 `fmt` import）：

```go
info := p.portal.BindingInfo()
switch info.Mode {
case "interface":
	text += fmt.Sprintf("｜接口绑定 %s(#%d)", info.Interface, info.Index)
case "source_only":
	if info.Degraded {
		text += "｜降级：仅绑源 IP"
	} else {
		text += "｜仅绑源 IP"
	}
}
if info.Err != nil {
	text += "｜接口绑定失败：" + info.Err.Error()
}
```

- [ ] **Step 4: 运行 API 与 app 测试**

Run: `go -C go-cnal test ./internal/api ./internal/app -count=1`

Expected: PASS；旧 JSON 字段不变，新字段 omitempty。

- [ ] **Step 5: 提交**

```bash
git add go-cnal/internal/api/api.go go-cnal/internal/api/api_test.go go-cnal/internal/app/compose_api.go go-cnal/internal/app/portalroute.go go-cnal/internal/app/portalroute_test.go
git commit -m "feat(api): 状态快照暴露接口绑定与降级诊断"
```

---

### Task 3: 把绑定接入 DNS 与门户 TLS 探测

**Files:**
- Modify: `go-cnal/internal/netstack/resolver.go:23-45,105-117`
- Modify: `go-cnal/internal/netstack/portalprobe.go:25-54`
- Test: `go-cnal/internal/netstack/resolver_test.go`
- Test: `go-cnal/internal/netstack/portalprobe_test.go`（若不存在则创建）

- [ ] **Step 1: 写失败测试**

```go
func TestResolveBoundStrictInterfaceFailureSendsNoDNS(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	interfaceSnapshots = func() ([]interfaceSnapshot, error) { return nil, nil }
	server := fakeDNS(t, net.ParseIP("1.1.1.2"))
	if _, err := ResolveBoundWithBindMode(net.ParseIP("127.0.0.1"), server, "portal.example.test", time.Second, BindModeInterface); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("严格接口失败应返回 ErrInterfaceBind，实际 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go -C go-cnal test ./internal/netstack -run TestResolveBoundStrictInterfaceFailureSendsNoDNS -count=1`

Expected: 编译失败，`ResolveBoundWithBindMode` 不存在。

- [ ] **Step 3: 实现 DNS 与门户探测变体**

`resolver.go` 保留原函数作为默认包装：

```go
func ResolveBound(src net.IP, dnsServer, host string, timeout time.Duration) (net.IP, error) {
	return ResolveBoundWithBindMode(src, dnsServer, host, timeout, DefaultBindMode())
}

func ResolveBoundWithBindMode(src net.IP, dnsServer, host string, timeout time.Duration, mode BindMode) (net.IP, error) {
	if src == nil {
		return nil, errors.New("未绑定源地址：物理栈纪律不允许")
	}
	dnsServer = normalizeDNSServer(dnsServer)
	if mode == BindModeInterface {
		dnsHost, _, err := net.SplitHostPort(dnsServer)
		if err != nil || net.ParseIP(dnsHost) == nil {
			return nil, fmt.Errorf("%w: 严格模式 campus_dns 必须是 IP 字面量", ErrInterfaceBind)
		}
	}
	network := "udp4"
	if src.To4() == nil {
		network = "udp6"
	}
	d, _, err := makeDialer(src, network, timeout, mode)
	if err != nil {
		return nil, fmt.Errorf("绑源 DNS 拨号失败: %w", err)
	}
	res := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, network, dnsServer)
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ips, err := res.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return pickPortalIP(ips)
}
```

`portalprobe.go`：

```go
func ProbePortal(src net.IP, dnsServer, host, port string, timeout time.Duration, verifyChain bool) error {
	return ProbePortalWithBindMode(src, dnsServer, host, port, timeout, verifyChain, DefaultBindMode())
}

func ProbePortalWithBindMode(src net.IP, dnsServer, host, port string, timeout time.Duration, verifyChain bool, mode BindMode) error {
	if src == nil {
		return errors.New("未绑定源地址：物理栈纪律不允许")
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ip, err := ResolveBoundWithBindMode(src, dnsServer, host, timeout, mode)
	if err != nil {
		return fmt.Errorf("解析门户地址失败: %w", err)
	}
	addr := net.JoinHostPort(ip.String(), port)
	d, _, err := makeDialer(src, "tcp", timeout, mode)
	if err != nil {
		return fmt.Errorf("创建门户拨号器失败: %w", err)
	}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("连门户 %s 失败: %w", addr, err)
	}
	defer conn.Close()
	cfg := &tls.Config{ServerName: host}
	if !verifyChain {
		cfg.InsecureSkipVerify = true //nolint:gosec // 用户显式配置；主机名另有关验
		cfg.VerifyPeerCertificate = hostOnlyVerifier(host)
	}
	tc := tls.Client(conn, cfg)
	tc.SetDeadline(time.Now().Add(timeout))
	if err := tc.Handshake(); err != nil {
		return fmt.Errorf("门户 TLS 握手失败（%s）: %w", addr, err)
	}
	return nil
}
```

- [ ] **Step 4: 运行 netstack 全量测试**

Run: `go -C go-cnal test ./internal/netstack -count=1`

Expected: PASS；fake DNS、fake-ip 过滤、门户探测回归不漂移。

- [ ] **Step 5: 提交**

```bash
git add go-cnal/internal/netstack/resolver.go go-cnal/internal/netstack/portalprobe.go go-cnal/internal/netstack/resolver_test.go go-cnal/internal/netstack/portalprobe_test.go
git commit -m "fix(netstack): DNS 与门户探测共用接口绑定"
```

---
