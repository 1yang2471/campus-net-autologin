//go:build windows

package netstack

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsInterfaceOptionReadback(t *testing.T) {
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
	// Windows 回读已把接口索引转换回主机字节序。
	want := iface.Index
	if got != want {
		t.Fatalf("接口选项回读错误，实际 %#x 期望 %#x", got, want)
	}
}

func TestWindowsInterfaceControlRejectsZeroIndex(t *testing.T) {
	if _, err := interfaceControl(0, net.ParseIP("127.0.0.1")); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("索引 0 必须拒绝，实际 %v", err)
	}
}

func TestWindowsInterfaceOptionWrapsSocketFailure(t *testing.T) {
	if err := setInterfaceOption(windows.Handle(^uintptr(0)), 1, net.ParseIP("127.0.0.1")); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("socket option 失败必须归入 ErrInterfaceBind，实际 %v", err)
	}
}

func TestResolveBoundDiscoversInterfaceDNS(t *testing.T) {
	old := interfaceDNSDiscoverer
	t.Cleanup(func() { interfaceDNSDiscoverer = old })
	server := fakeDNS(t, net.ParseIP("198.51.100.10"))
	interfaceDNSDiscoverer = func(net.IP) (string, error) { return server, nil }
	ip, err := ResolveBoundWithBindMode(net.ParseIP("127.0.0.1"), "", "portal.example.test", 2*time.Second, BindModeInterface)
	if err != nil || !ip.Equal(net.ParseIP("198.51.100.10")) {
		t.Fatalf("空 campus_dns 应使用接口 DNS，实际 %v %v", ip, err)
	}
}

func TestDiscoverDNSServerFromPhysicalSource(t *testing.T) {
	src := DiscoverSource("")
	if src == nil {
		t.Skip("本机无物理源")
	}
	server, err := discoverDNSServer(src)
	if err != nil {
		t.Skipf("本机接口未提供可用 DNS: %v", err)
	}
	host, _, err := net.SplitHostPort(server)
	if err != nil || net.ParseIP(host) == nil {
		t.Fatalf("接口 DNS 应为 IP 字面量，实际 %q %v", server, err)
	}
}

func TestPickDNSServerUsesInterfaceAndAddressFamily(t *testing.T) {
	raw := windows.RawSockaddrInet4{Family: windows.AF_INET}
	copy(raw.Addr[:], net.ParseIP("192.0.2.53").To4())
	dnsAddr := windows.SocketAddress{
		Sockaddr:       (*syscall.RawSockaddrAny)(unsafe.Pointer(&raw)),
		SockaddrLength: int32(unsafe.Sizeof(raw)),
	}
	head := &windows.IpAdapterAddresses{
		IfIndex: 7,
		FirstDnsServerAddress: &windows.IpAdapterDnsServerAdapter{
			Address: dnsAddr,
		},
	}
	got, err := pickDNSServer(head, 7, true)
	if err != nil || got != "192.0.2.53:53" {
		t.Fatalf("应取接口 7 的 IPv4 DNS，实际 %q %v", got, err)
	}
	if _, err := pickDNSServer(head, 8, true); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("接口不匹配必须 fail-closed，实际 %v", err)
	}
	if _, err := pickDNSServer(head, 7, false); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("地址族不匹配必须 fail-closed，实际 %v", err)
	}
}
