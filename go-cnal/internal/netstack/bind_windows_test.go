//go:build windows

package netstack

import (
	"errors"
	"net"
	"testing"

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
