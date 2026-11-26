package netstack

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestProbePortalStrictInterfaceFailure(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	interfaceSnapshots = func() ([]interfaceSnapshot, error) { return nil, nil }
	err := ProbePortalWithBindMode(net.ParseIP("127.0.0.1"), "127.0.0.1:53", "portal.example.test", "443", time.Second, true, BindModeInterface)
	if !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("严格接口失败应返回 ErrInterfaceBind，实际 %v", err)
	}
}
