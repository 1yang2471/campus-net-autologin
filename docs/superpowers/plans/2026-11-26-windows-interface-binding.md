# Windows 接口级绑网 Implementation Plan

> **For agentic workers:** Implement this plan task-by-task (use a subagent-driven-development or executing-plans skill if one is installed). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Windows 校园网探测、DNS 与登录 socket 真正绑定物理接口，默认严格阻止凭据回退系统栈，并补齐仅本机回环的 Clash fake-ip flush，最终发布 `v2.4.2-go` Windows onedir。

**Architecture:** 在 `internal/netstack` 收敛“源 IP + 接口索引 + socket Control”三件绑定事实；Windows 通过 `IP_UNICAST_IF`/`IPV6_UNICAST_IF` 在 connect/bind 前钉接口。组合根按 `strict_interface_bind` 选择 interface/auto/source_only 模式，严格模式下登录选路 fail-closed。Clash API 作为登录成功后的非阻断后置动作。

**Tech Stack:** Go 1.27、`net.Dialer.Control`、`net.ListenConfig.Control`、`golang.org/x/sys/windows`、PowerShell 打包与 GitHub Release。

---

## File Structure

- `go-cnal/internal/netstack/binding.go`：跨平台绑定模式、接口解析、拨号/Listen 辅助与状态。
- `go-cnal/internal/netstack/bind_windows.go`：Windows socket option 与字节序。
- `go-cnal/internal/netstack/bind_other.go`：非 Windows no-op，保持 Android 行为。
- `go-cnal/internal/netstack/client.go`：Client 绑定模式、dialer、Bindable、状态导出。
- `go-cnal/internal/netstack/resolver.go`：绑接口 DNS 解析。
- `go-cnal/internal/netstack/portalprobe.go`：绑接口门户 TLS 探测。
- `go-cnal/internal/app/config.go`：严格模式与 Clash API 配置。
- `go-cnal/internal/app/compose_portal.go`：组合根绑定模式、Clash 客户端、改绑。
- `go-cnal/internal/app/portalroute.go`：严格 fail-closed、错误分类、路径文本。
- `go-cnal/internal/app/detect.go`、`channel_probe.go`、`channel_login.go`：物理路径使用同一绑定模式。
- `go-cnal/internal/clashapi/client.go`：仅回环 fake-ip flush 客户端。
- `go-cnal/internal/keeper/login.go`：登录成功回调，统一触发 flush。
- `go-cnal/internal/api/api.go`、`compose_api.go`：可选绑定诊断字段。
- `go-cnal/scripts/build.ps1`、README、FAQ、兼容性模板、当日复盘：版本与发布同步。

---

## Plan Parts

- Tasks 1–2：本文件。
- Tasks 3–6：[`2026-11-26-windows-interface-binding-tasks-3-6.md`](2026-11-26-windows-interface-binding-tasks-3-6.md)。
- Tasks 7–9：[`2026-11-26-windows-interface-binding-tasks-7-9.md`](2026-11-26-windows-interface-binding-tasks-7-9.md)。

### Task 1: 建立接口绑定核心与 Windows socket option

**Files:**
- Create: `go-cnal/internal/netstack/binding.go`
- Create: `go-cnal/internal/netstack/bind_windows.go`
- Create: `go-cnal/internal/netstack/bind_other.go`
- Test: `go-cnal/internal/netstack/binding_test.go`
- Test: `go-cnal/internal/netstack/bind_windows_test.go`

- [ ] **Step 1: 写失败测试**

`binding_test.go`：

```go
package netstack

import (
	"errors"
	"net"
	"testing"
)

func TestResolveInterfaceRejectsMissingAndDuplicate(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	_, ipnet, _ := net.ParseCIDR("192.0.2.10/24")
	interfaceSnapshots = func() ([]interfaceSnapshot, error) {
		return []interfaceSnapshot{{Index: 7, Name: "eth0", Up: true, Addrs: []net.IPNet{*ipnet}}}, nil
	}
	got, err := resolveInterface(net.ParseIP("192.0.2.10"))
	if err != nil || got.Index != 7 {
		t.Fatalf("唯一匹配应返回接口 7，实际 %+v %v", got, err)
	}
	interfaceSnapshots = func() ([]interfaceSnapshot, error) {
		x := []interfaceSnapshot{{Index: 7, Name: "eth0", Up: true, Addrs: []net.IPNet{*ipnet}}}
		return append(x, interfaceSnapshot{Index: 8, Name: "eth1", Up: true, Addrs: []net.IPNet{*ipnet}}), nil
	}
	if _, err := resolveInterface(net.ParseIP("192.0.2.10")); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("同 IP 多接口必须 fail-closed，实际 %v", err)
	}
}

func TestAutoModeFallsBackButInterfaceModeDoesNot(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	interfaceSnapshots = func() ([]interfaceSnapshot, error) { return nil, nil }
	src := net.ParseIP("192.0.2.10")
	if _, _, err := makeDialer(src, "tcp4", 1e9, BindModeInterface); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("interface 模式不得降级，实际 %v", err)
	}
	d, info, err := makeDialer(src, "tcp4", 1e9, BindModeAuto)
	if err != nil || d == nil || !info.Degraded || info.Mode != "source_only" {
		t.Fatalf("auto 模式应显式降级，实际 d=%v info=%+v err=%v", d, info, err)
	}
}
```

`bind_windows_test.go`：

```go
//go:build windows

package netstack

import (
	"math/bits"
	"net"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsInterfaceOptionByteOrderAndReadback(t *testing.T) {
	iface, err := resolveInterface(net.ParseIP("127.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	fd, err := windows.Socket(windows.AF_INET, windows.SOCK_STREAM, windows.IPPROTO_IP)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.Close(fd)
	if err := setInterfaceOption(fd, iface.Index, net.ParseIP("127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	got, err := windows.GetsockoptInt(fd, windows.IPPROTO_IP, ipUnicastIF)
	if err != nil {
		t.Fatal(err)
	}
	want := int(bits.ReverseBytes32(uint32(iface.Index)))
	if got != want {
		t.Fatalf("IPv4 接口索引必须按网络字节序写入，实际 %#x 期望 %#x", got, want)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go -C go-cnal test ./internal/netstack -run 'TestResolveInterface|TestAutoMode|TestWindowsInterfaceOption' -count=1`

Expected: 编译失败，`ErrInterfaceBind`、`BindMode`、`makeDialer` 尚未定义。

- [ ] **Step 3: 实现 `binding.go`**

核心类型与函数：

```go
package netstack

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

var ErrInterfaceBind = errors.New("接口绑定失败")

type BindMode uint8

const (
	BindModeAuto BindMode = iota
	BindModeInterface
	BindModeSourceOnly
)

type BindingInfo struct {
	Mode      string
	Interface string
	Index     int
	Degraded  bool
	Err       error
}

type interfaceSnapshot struct {
	Index int
	Name  string
	Up    bool
	Addrs []net.IPNet
}

var interfaceSnapshots = loadInterfaceSnapshots

func loadInterfaceSnapshots() ([]interfaceSnapshot, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]interfaceSnapshot, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		s := interfaceSnapshot{Index: iface.Index, Name: iface.Name, Up: iface.Flags&net.FlagUp != 0}
		for _, addr := range addrs {
			if n, ok := addr.(*net.IPNet); ok {
				s.Addrs = append(s.Addrs, *n)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func DefaultBindMode() BindMode {
	if interfaceBindingSupported() {
		return BindModeInterface
	}
	return BindModeSourceOnly
}

func ModeForStrict(strict bool) BindMode {
	if !interfaceBindingSupported() {
		return BindModeSourceOnly
	}
	if strict {
		return BindModeInterface
	}
	return BindModeAuto
}

func resolveInterface(src net.IP) (interfaceSnapshot, error) {
	if src == nil {
		return interfaceSnapshot{}, fmt.Errorf("%w: 源 IP 为空", ErrInterfaceBind)
	}
	snaps, err := interfaceSnapshots()
	if err != nil {
		return interfaceSnapshot{}, fmt.Errorf("%w: 枚举接口失败: %v", ErrInterfaceBind, err)
	}
	var found interfaceSnapshot
	matched := 0
	for _, s := range snaps {
		for _, n := range s.Addrs {
			if n.IP.Equal(src) {
				matched++
				found = s
			}
		}
	}
	if matched == 0 {
		return interfaceSnapshot{}, fmt.Errorf("%w: 源 %s 不属于任何本机接口", ErrInterfaceBind, src)
	}
	if matched > 1 {
		return interfaceSnapshot{}, fmt.Errorf("%w: 源 %s 出现在多个接口", ErrInterfaceBind, src)
	}
	if !found.Up {
		return interfaceSnapshot{}, fmt.Errorf("%w: 接口 %s 未启用", ErrInterfaceBind, found.Name)
	}
	if found.Index <= 0 {
		return interfaceSnapshot{}, fmt.Errorf("%w: 接口 %s 索引无效", ErrInterfaceBind, found.Name)
	}
	return found, nil
}

func socketControl(src net.IP, mode BindMode) (func(string, string, syscall.RawConn) error, BindingInfo, error) {
	if src == nil {
		return nil, BindingInfo{}, errors.New("未绑定源地址：物理栈纪律不允许")
	}
	if mode == BindModeSourceOnly || (mode == BindModeAuto && !interfaceBindingSupported()) {
		return nil, BindingInfo{Mode: "source_only"}, nil
	}
	iface, err := resolveInterface(src)
	info := BindingInfo{Interface: iface.Name, Index: iface.Index}
	if err != nil {
		info.Err = err
		if mode == BindModeAuto {
			info.Mode = "source_only"
			info.Degraded = true
			return nil, info, nil
		}
		return nil, info, err
	}
	control, err := interfaceControl(iface.Index, src)
	if err != nil {
		info.Err = err
		if mode == BindModeAuto {
			info.Mode = "source_only"
			info.Degraded = true
			return nil, info, nil
		}
		return nil, info, fmt.Errorf("%w: %s(#%d): %v", ErrInterfaceBind, iface.Name, iface.Index, err)
	}
	info.Mode = "interface"
	return control, info, nil
}

func makeDialer(src net.IP, network string, timeout time.Duration, mode BindMode) (*net.Dialer, BindingInfo, error) {
	control, info, err := socketControl(src, mode)
	if err != nil {
		return nil, info, err
	}
	d := &net.Dialer{Timeout: timeout, Control: control}
	switch network {
	case "tcp", "tcp4", "tcp6":
		d.LocalAddr = &net.TCPAddr{IP: src}
	case "udp", "udp4", "udp6":
		d.LocalAddr = &net.UDPAddr{IP: src}
	default:
		return nil, info, fmt.Errorf("不支持的拨号网络: %s", network)
	}
	return d, info, nil
}

func bindableListener(src net.IP, mode BindMode) (net.ListenConfig, BindingInfo, error) {
	control, info, err := socketControl(src, mode)
	return net.ListenConfig{Control: control}, info, err
}
```

- [ ] **Step 4: 实现 Windows 与其它平台文件**

`bind_windows.go`：

```go
//go:build windows

package netstack

import (
	"fmt"
	"math/bits"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

const ipUnicastIF = 31

func interfaceBindingSupported() bool { return true }

func setInterfaceOption(fd windows.Handle, index int, src net.IP) error {
	if index <= 0 {
		return fmt.Errorf("%w: 接口索引为 0", ErrInterfaceBind)
	}
	if src.To4() != nil {
		return windows.SetsockoptInt(fd, windows.IPPROTO_IP, ipUnicastIF, int(bits.ReverseBytes32(uint32(index))))
	}
	return windows.SetsockoptInt(fd, windows.IPPROTO_IPV6, ipUnicastIF, index)
}

func interfaceControl(index int, src net.IP) (func(string, string, syscall.RawConn) error, error) {
	if index <= 0 {
		return nil, fmt.Errorf("%w: 接口索引为 0", ErrInterfaceBind)
	}
	return func(_, _ string, c syscall.RawConn) error {
		var sockErr error
		if err := c.Control(func(fd uintptr) {
			sockErr = setInterfaceOption(windows.Handle(fd), index, src)
		}); err != nil {
			return err
		}
		return sockErr
	}, nil
}
```

`bind_other.go`：

```go
//go:build !windows

package netstack

import (
	"errors"
	"net"
	"syscall"
)

func interfaceBindingSupported() bool { return false }

func interfaceControl(int, net.IP) (func(string, string, syscall.RawConn) error, error) {
	return nil, errors.New("当前平台不支持接口级绑定")
}
```

- [ ] **Step 5: 运行测试并修正到通过**

Run: `go -C go-cnal test ./internal/netstack -run 'TestResolveInterface|TestAutoMode|TestWindowsInterfaceOption' -count=1`

Expected: PASS；Windows 回读断言证明 IPv4 网络字节序正确。

- [ ] **Step 6: 提交**

```bash
git add go-cnal/internal/netstack/binding.go go-cnal/internal/netstack/bind_windows.go go-cnal/internal/netstack/bind_other.go go-cnal/internal/netstack/binding_test.go go-cnal/internal/netstack/bind_windows_test.go go-cnal/go.mod go-cnal/go.sum
git commit -m "feat(netstack): 增加 Windows 接口级 socket 绑定"
```

---

### Task 2: 把绑定接入 Client 与 Bindable

**Files:**
- Modify: `go-cnal/internal/netstack/client.go:24-90,185-196`
- Test: `go-cnal/internal/netstack/client_test.go`
- Test: `go-cnal/internal/netstack/bindable_test.go`
- Test: `go-cnal/internal/netstack/client_system_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestClientReportsInterfaceBinding(t *testing.T) {
	c := New(net.IPv4(127, 0, 0, 1), time.Second)
	if !c.Bindable(nil) {
		t.Fatal("回环接口应可绑")
	}
	info := c.BindingInfo()
	if info.Mode != "interface" || info.Index <= 0 {
		t.Fatalf("应报告真实接口绑定，实际 %+v", info)
	}
}

func TestClientStrictBindFailureDoesNotDial(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	interfaceSnapshots = func() ([]interfaceSnapshot, error) { return nil, nil }
	c := NewWithBindMode(net.IPv4(127, 0, 0, 1), time.Second, BindModeInterface)
	if c.Bindable(nil) {
		t.Fatal("严格模式接口解析失败必须判不可绑")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := c.Get("http://"+ln.Addr().String()+"/", nil, ""); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("严格模式不得触网，实际 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go -C go-cnal test ./internal/netstack -run 'TestClientReportsInterfaceBinding|TestClientStrictBindFailureDoesNotDial' -count=1`

Expected: 编译失败，`NewWithBindMode` 与 `BindingInfo` 方法不存在。

- [ ] **Step 3: 修改 Client**

在 `Client` 增加：

```go
bindMode BindMode

bindMu   sync.Mutex
lastBind BindingInfo
```

构造与模式：

```go
func New(src net.IP, timeout time.Duration) *Client {
	return NewWithBindMode(src, timeout, DefaultBindMode())
}

func NewWithBindMode(src net.IP, timeout time.Duration, mode BindMode) *Client {
	return &Client{src: src, perTry: timeout, bindMode: mode}
}

func (c *Client) SetBindMode(mode BindMode) {
	c.mu.Lock()
	c.bindMode = mode
	c.mu.Unlock()
}

func (c *Client) noteBind(info BindingInfo) {
	c.bindMu.Lock()
	c.lastBind = info
	c.bindMu.Unlock()
}

func (c *Client) BindingInfo() BindingInfo {
	c.bindMu.Lock()
	defer c.bindMu.Unlock()
	return c.lastBind
}
```

`Bindable` 改为：

```go
func (c *Client) Bindable(src net.IP) bool {
	if c.system {
		return true
	}
	if src == nil {
		src = c.source()
	}
	if src == nil {
		return false
	}
	c.mu.RLock()
	mode := c.bindMode
	c.mu.RUnlock()
	lc, info, err := bindableListener(src, mode)
	c.noteBind(info)
	if err != nil {
		return false
	}
	network := "udp4"
	if src.To4() == nil {
		network = "udp6"
	}
	pc, err := lc.ListenPacket(context.Background(), network, net.JoinHostPort(src.String(), "0"))
	if err != nil {
		info.Err = err
		c.noteBind(info)
		return false
	}
	_ = pc.Close()
	return true
}
```

`dialer` 改为：

```go
func (c *Client) dialer() (*net.Dialer, error) {
	if c.system {
		return &net.Dialer{Timeout: c.perTry}, nil
	}
	src := c.source()
	if src == nil {
		return nil, errors.New("未绑定源地址：物理栈纪律不允许")
	}
	c.mu.RLock()
	mode := c.bindMode
	c.mu.RUnlock()
	d, info, err := makeDialer(src, "tcp", c.perTry, mode)
	c.noteBind(info)
	return d, err
}
```

- [ ] **Step 4: 运行 netstack 全量测试**

Run: `go -C go-cnal test ./internal/netstack -count=1`

Expected: PASS；现有环回 HTTP、SetSource、系统栈测试继续通过。

- [ ] **Step 5: 提交**

```bash
git add go-cnal/internal/netstack/client.go go-cnal/internal/netstack/client_test.go go-cnal/internal/netstack/bindable_test.go go-cnal/internal/netstack/client_system_test.go
git commit -m "fix(netstack): HTTP 与可绑定性自检统一绑定接口"
```

---
