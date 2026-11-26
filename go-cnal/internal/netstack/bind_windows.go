//go:build windows

package netstack

import (
	"fmt"
	"math/bits"
	"net"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ipUnicastIF = 31

func discoverDNSServer(src net.IP) (string, error) {
	iface, err := resolveInterface(src)
	if err != nil {
		return "", err
	}
	family := uint32(windows.AF_INET)
	if src.To4() == nil {
		family = windows.AF_INET6
	}
	flags := uint32(windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST | windows.GAA_FLAG_SKIP_FRIENDLY_NAME)
	var size uint32
	if err := windows.GetAdaptersAddresses(family, flags, 0, nil, &size); err != windows.ERROR_BUFFER_OVERFLOW {
		return "", fmt.Errorf("%w: 读取接口 DNS 失败: %v", ErrInterfaceBind, err)
	}
	if size == 0 {
		return "", fmt.Errorf("%w: 接口 DNS 地址列表为空", ErrInterfaceBind)
	}
	buf := make([]byte, size)
	var pinner runtime.Pinner
	pinner.Pin(&buf[0])
	defer pinner.Unpin()
	addresses := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
	if err := windows.GetAdaptersAddresses(family, flags, 0, addresses, &size); err != nil {
		return "", fmt.Errorf("%w: 读取接口 DNS 失败: %v", ErrInterfaceBind, err)
	}
	server, err := pickDNSServer(addresses, iface.Index, src.To4() != nil)
	runtime.KeepAlive(buf)
	return server, err
}

func pickDNSServer(head *windows.IpAdapterAddresses, index int, ipv4 bool) (string, error) {
	for adapter := head; adapter != nil; adapter = adapter.Next {
		adapterIndex := int(adapter.IfIndex)
		if !ipv4 && adapter.Ipv6IfIndex != 0 {
			adapterIndex = int(adapter.Ipv6IfIndex)
		}
		if adapterIndex != index {
			continue
		}
		for dns := adapter.FirstDnsServerAddress; dns != nil; dns = dns.Next {
			ip := dns.Address.IP()
			if ip == nil || ip.IsUnspecified() || (ipv4 && ip.To4() == nil) || (!ipv4 && ip.To4() != nil) {
				continue
			}
			return net.JoinHostPort(ip.String(), "53"), nil
		}
	}
	return "", fmt.Errorf("%w: 接口 #%d 未配置同族 DNS", ErrInterfaceBind, index)
}

func interfaceBindingSupported() bool { return true }

func setInterfaceOption(fd windows.Handle, index int, src net.IP) error {
	if index <= 0 {
		return fmt.Errorf("%w: 接口索引为 0", ErrInterfaceBind)
	}
	if src.To4() != nil {
		// Windows 的 IPv4 IP_UNICAST_IF 要求接口索引按网络字节序写入；
		// IPv6 IPV6_UNICAST_IF 使用主机字节序。
		if err := windows.SetsockoptInt(fd, windows.IPPROTO_IP, ipUnicastIF,
			int(bits.ReverseBytes32(uint32(index)))); err != nil {
			return fmt.Errorf("%w: IPv4 IP_UNICAST_IF: %v", ErrInterfaceBind, err)
		}
		return nil
	}
	if err := windows.SetsockoptInt(fd, windows.IPPROTO_IPV6, ipUnicastIF, index); err != nil {
		return fmt.Errorf("%w: IPv6 IPV6_UNICAST_IF: %v", ErrInterfaceBind, err)
	}
	return nil
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
