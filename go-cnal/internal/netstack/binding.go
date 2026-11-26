package netstack

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

// ErrInterfaceBind 表示源 IP 无法唯一绑定到本机接口，或 Windows socket
// 接口选项设置失败。调用方不得把这种失败当作普通 DNS/连接超时。
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
		if src.IsLoopback() {
			var loopback interfaceSnapshot
			loopbacks := 0
			for _, s := range snaps {
				for _, n := range s.Addrs {
					if n.IP.IsLoopback() {
						loopbacks++
						loopback = s
						break
					}
				}
			}
			if loopbacks == 1 && loopback.Up && loopback.Index > 0 {
				return loopback, nil
			}
		}
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
