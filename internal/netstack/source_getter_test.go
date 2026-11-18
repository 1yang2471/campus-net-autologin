package netstack

import (
	"net"
	"testing"
	"time"
)

// 行为：Source() 读当前绑定源（面板 IPv4 行显示实际物理源——TUN/代理环境下
// 「我到底从哪张网卡出去」是用户最该看到的事实，重发现改绑后读数同步变）。
func TestSourceReflectsBoundAndUpdated(t *testing.T) {
	c := New(net.ParseIP("172.29.1.5"), time.Second)
	if got := c.Source().String(); got != "172.29.1.5" {
		t.Errorf("初始源应为绑定值，实际 %q", got)
	}
	c.SetSource(net.ParseIP("10.0.0.9"))
	if got := c.Source().String(); got != "10.0.0.9" {
		t.Errorf("SetSource 后应读到新源，实际 %q", got)
	}
}
