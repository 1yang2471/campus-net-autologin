package netstack

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestResolveInterfaceFindsUniqueSource(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	_, ipnet, _ := net.ParseCIDR("192.0.2.10/24")
	ipnet.IP = net.ParseIP("192.0.2.10")
	interfaceSnapshots = func() ([]interfaceSnapshot, error) {
		return []interfaceSnapshot{{Index: 7, Name: "eth0", Up: true, Addrs: []net.IPNet{*ipnet}}}, nil
	}
	got, err := resolveInterface(net.ParseIP("192.0.2.10"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Index != 7 || got.Name != "eth0" {
		t.Fatalf("唯一匹配应返回接口 7/eth0，实际 %+v", got)
	}
}

func TestResolveInterfaceRejectsAmbiguousOrUnavailable(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	_, ipnet, _ := net.ParseCIDR("192.0.2.10/24")
	ipnet.IP = net.ParseIP("192.0.2.10")
	src := net.ParseIP("192.0.2.10")
	cases := []struct {
		name  string
		snaps []interfaceSnapshot
	}{
		{name: "missing", snaps: nil},
		{name: "duplicate", snaps: []interfaceSnapshot{
			{Index: 7, Name: "eth0", Up: true, Addrs: []net.IPNet{*ipnet}},
			{Index: 8, Name: "eth1", Up: true, Addrs: []net.IPNet{*ipnet}},
		}},
		{name: "down", snaps: []interfaceSnapshot{{Index: 7, Name: "eth0", Up: false, Addrs: []net.IPNet{*ipnet}}}},
		{name: "zero index", snaps: []interfaceSnapshot{{Index: 0, Name: "eth0", Up: true, Addrs: []net.IPNet{*ipnet}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			interfaceSnapshots = func() ([]interfaceSnapshot, error) { return tc.snaps, nil }
			if _, err := resolveInterface(src); !errors.Is(err, ErrInterfaceBind) {
				t.Fatalf("应返回 ErrInterfaceBind，实际 %v", err)
			}
		})
	}
	if _, err := resolveInterface(nil); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("nil 源应返回 ErrInterfaceBind，实际 %v", err)
	}
}

func TestResolveInterfaceAcceptsLoopbackAlias(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	_, ipnet, _ := net.ParseCIDR("127.0.0.1/8")
	interfaceSnapshots = func() ([]interfaceSnapshot, error) {
		return []interfaceSnapshot{{Index: 1, Name: "Loopback Pseudo-Interface 1", Up: true, Addrs: []net.IPNet{*ipnet}}}, nil
	}
	got, err := resolveInterface(net.ParseIP("127.0.0.2"))
	if err != nil || got.Index != 1 {
		t.Fatalf("127/8 别名应绑定回环接口，实际 %+v %v", got, err)
	}
}

func TestModeForStrictSelectsInterfaceOrSourceOnly(t *testing.T) {
	want := BindModeSourceOnly
	if DefaultBindMode() == BindModeInterface {
		want = BindModeInterface
	}
	if ModeForStrict(true) != want {
		t.Fatalf("严格模式应选择 interface，实际 %v", ModeForStrict(true))
	}
	if ModeForStrict(false) != BindModeSourceOnly {
		t.Fatalf("非严格模式必须直接 source_only，实际 %v", ModeForStrict(false))
	}
}

func TestBindModeStrictFailsClosedAndSourceOnlyDegrades(t *testing.T) {
	old := interfaceSnapshots
	t.Cleanup(func() { interfaceSnapshots = old })
	interfaceSnapshots = func() ([]interfaceSnapshot, error) { return nil, nil }
	src := net.ParseIP("192.0.2.10")
	if _, _, err := makeDialer(src, "tcp4", time.Second, BindModeInterface); !errors.Is(err, ErrInterfaceBind) {
		t.Fatalf("interface 模式不得降级，实际 %v", err)
	}
	d, info, err := makeDialer(src, "tcp4", time.Second, BindModeSourceOnly)
	wantDegraded := interfaceBindingSupported()
	if err != nil || d == nil || info.Degraded != wantDegraded || info.Mode != "source_only" {
		t.Fatalf("source_only 模式应显式降级，实际 d=%v info=%+v err=%v", d, info, err)
	}
	if d.Control != nil {
		t.Fatal("降级后不得保留接口 Control")
	}
}
