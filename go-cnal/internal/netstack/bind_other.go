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
