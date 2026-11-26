package netstack

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestClientReportsInterfaceBinding(t *testing.T) {
	c := New(net.IPv4(127, 0, 0, 1), time.Second)
	if !c.Bindable(nil) {
		t.Fatalf("回环接口应可绑，实际 %+v", c.BindingInfo())
	}
	info := c.BindingInfo()
	if DefaultBindMode() == BindModeInterface {
		if info.Mode != "interface" || info.Index <= 0 {
			t.Fatalf("应报告真实接口绑定，实际 %+v", info)
		}
	} else if info.Mode != "source_only" {
		t.Fatalf("非 Windows 应保持 source_only，实际 %+v", info)
	}
}

func TestCloneForSourceKeepsOperationSnapshot(t *testing.T) {
	c := New(net.ParseIP("127.0.0.1"), time.Second)
	snapshot := c.CloneForSource(net.ParseIP("192.0.2.10"))
	c.SetSource(net.ParseIP("127.0.0.2"))
	if !snapshot.Source().Equal(net.ParseIP("192.0.2.10")) {
		t.Fatalf("操作快照不得随共享客户端改绑，实际 %v", snapshot.Source())
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
