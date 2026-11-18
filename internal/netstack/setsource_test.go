package netstack

import (
	"io"
	"net"
	"testing"
	"time"
)

// 源可在运行时替换（物理源重发现用）：网络切换/休眠唤醒后旧源失效，
// 组合根重发现新源后须能改绑——否则守护会一直用死源（Python bind_ip
// 重发现同源需求）。手动重连走电路 API 的 goroutine，故必须并发安全。

// 行为：SetSource 后请求从新源发出（服务端看到的对端地址随之变化）。
func TestSetSourceSwitchesBindAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srcs := make(chan string, 2)
	go func() {
		for range 2 {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srcs <- conn.RemoteAddr().(*net.TCPAddr).IP.String()
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			_, _ = io.ReadFull(conn, make([]byte, 4096))
			_ = conn.Close()
		}
	}()

	c := New(net.IPv4(127, 0, 0, 1), 2*time.Second)
	if _, err := c.Get("http://"+ln.Addr().String()+"/x", nil, ""); err != nil {
		t.Fatalf("首次请求失败: %v", err)
	}
	c.SetSource(net.IPv4(127, 0, 0, 2))
	if _, err := c.Get("http://"+ln.Addr().String()+"/x", nil, ""); err != nil {
		t.Fatalf("换源后请求失败: %v", err)
	}
	if got := <-srcs; got != "127.0.0.1" {
		t.Errorf("首次源应为 127.0.0.1，实际 %s", got)
	}
	if got := <-srcs; got != "127.0.0.2" {
		t.Errorf("换源后源应为 127.0.0.2，实际 %s", got)
	}
}

// 行为：并发读写源不得触发竞态（-race 下跑）。
func TestSetSourceConcurrentSafe(t *testing.T) {
	c := New(net.IPv4(127, 0, 0, 1), time.Second)
	done := make(chan struct{})
	go func() {
		for i := range 200 {
			_ = c.source()
			_ = i
		}
		close(done)
	}()
	for i := range 200 {
		c.SetSource(net.IPv4(127, 0, 0, byte(i%200)))
	}
	<-done
}
