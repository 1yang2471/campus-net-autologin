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
		// Windows 的 IPv4 IP_UNICAST_IF 要求接口索引按网络字节序写入；
		// IPv6 IPV6_UNICAST_IF 使用主机字节序。
		return windows.SetsockoptInt(fd, windows.IPPROTO_IP, ipUnicastIF,
			int(bits.ReverseBytes32(uint32(index))))
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
