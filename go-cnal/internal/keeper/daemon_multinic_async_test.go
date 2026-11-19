package keeper

import (
	"testing"
	"time"
)

// 回归（2026-11-19 下午）：probeSiblings 曾在 Tick 里同步跑网络 I/O——
// 网线插着但未认证时，每张旁路卡要花 probe_timeout 解析 DNS + 一次 HTTP，
// 两张卡最多 12 秒，把 3 秒心跳拖成十几秒一拍。行为判据：探测再慢，
// Tick 也必须立刻返回；观察结果异步补上。
func TestSlowSiblingProbeNeverBlocksTick(t *testing.T) {
	release := make(chan struct{})
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.ProbeSiblingAuth = func() []string {
		<-release // 模拟慢探测（网线未认证时的 DNS+HTTP 超时）
		return []string{"10.141.53.34"}
	}

	done := make(chan struct{})
	go func() {
		d.Tick()
		close(done)
	}()
	select {
	case <-done:
		// Tick 未被慢探测阻塞——正确
	case <-time.After(500 * time.Millisecond):
		close(release)
		t.Fatal("Tick 被慢探测阻塞：心跳节拍不得依赖旁路卡探测耗时")
	}
	close(release)

	// 探测完成后结果仍要能读到（异步补上，不是丢掉）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := d.UnauthSiblings(); len(got) == 1 && got[0] == "10.141.53.34" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("异步探测结果未落到读数，实际 %v", d.UnauthSiblings())
}

// 行为：慢探测还在飞的时候，后续拍不得再起一个（同一时刻最多一个探测在跑）。
func TestSiblingProbeSingleFlight(t *testing.T) {
	release := make(chan struct{})
	calls := make(chan struct{}, 10)
	d := NewDaemon("")
	d.ProbeOnline = func() bool { return true }
	d.PortalAlive = func() bool { return true }
	d.FetchFlux = func() (map[string]string, error) { return map[string]string{"用户名称": "u"}, nil }
	d.ProbeSiblingAuth = func() []string {
		calls <- struct{}{}
		<-release
		return nil
	}
	d.Tick() // 起第一个（异步，被 release 卡住）
	time.Sleep(50 * time.Millisecond)
	// 推进时钟越过 60s 节流，再拍几次：仍在飞的探测不得被重复启动。
	base := time.Now()
	for i := 1; i <= 3; i++ {
		off := time.Duration(i) * 61 * time.Second
		d.Now = func() time.Time { return base.Add(off) }
		d.Tick()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	time.Sleep(100 * time.Millisecond)
	if n := len(calls); n != 1 {
		t.Fatalf("同一时刻应只有一个探测在飞，实际启动 %d 次", n)
	}
}
