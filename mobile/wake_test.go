package mobile

import (
	"sync"
	"testing"
	"time"
)

// 事件驱动唤醒（复盘 §3.2）：Go tick 从「固定 ticker」改为「回调唤醒 +
// 5min 兜底」。Kotlin NetworkCallback（onLost/onCapabilitiesChanged 等）
// 调 Mobile.wake(reason) → 打断兜底等待立即完整 Tick。

// 行为：未启动时 Wake 是 no-op（nil 守卫），不 panic、不阻塞。
func TestWakeNoopWhenNotRunning(t *testing.T) {
	mu.Lock()
	saved := running
	running, stopCh = nil, nil
	mu.Unlock()
	defer func() {
		mu.Lock()
		running = saved
		mu.Unlock()
	}()

	done := make(chan struct{})
	go func() { Wake("test"); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("未启动时 Wake 阻塞了（应为 no-op）")
	}
}

// 行为：唤醒通道打断兜底等待——wakeCh 收到信号后，等待方能在兜底计时
// 远未到时就返回（即被打断）。这是 beatLoop 的核心语义。
func TestBeatWaitInterruptedByWake(t *testing.T) {
	wake := make(chan string, 4)
	begin := time.Now()
	go func() {
		time.Sleep(30 * time.Millisecond)
		wake <- "onCapabilitiesChanged"
	}()
	delay := beatWait(make(chan struct{}), wake, 5*time.Minute)
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Fatalf("唤醒应在兜底计时前打断，实际等了 %v", elapsed)
	}
	if delay != 0 {
		t.Fatalf("唤醒路径应立即可 Tick（delay=%v）", delay)
	}
}

// 行为：stop 必须能打断兜底等待——Stop() 要立即生效，不能等满兜底计时。
// （2026-11-13 发现：beatWait 不感知 stop，Stop 后 beatLoop 要等满剩余
// 兜底计时才退，旧 tickLoop 的 select 带 stop 分支没有此问题——回归。）
func TestBeatWaitInterruptedByStop(t *testing.T) {
	stop := make(chan struct{})
	wake := make(chan string, 1)
	close(stop)
	begin := time.Now()
	beatWait(stop, wake, 5*time.Minute)
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("stop 打断应立即返回，实际等了 %v", elapsed)
	}
}

// 行为：无唤醒时兜底等待自然到期。
func TestBeatWaitExpiresToFloor(t *testing.T) {
	begin := time.Now()
	delay := beatWait(make(chan struct{}), make(chan string, 1), 50*time.Millisecond)
	if elapsed := time.Since(begin); elapsed < 40*time.Millisecond {
		t.Fatalf("兜底应等满周期才返回，实际 %v", elapsed)
	}
	if delay != 50*time.Millisecond {
		t.Fatalf("兜底到期应返回完整周期 delay，实际 %v", delay)
	}
}

// 行为：连续多路唤醒不丢（缓冲通道 + 非阻塞投递），下一次 Tick 前至多
// 消费一个 reason，其余留在通道里不阻塞投递方。
func TestWakeReasonsBuffered(t *testing.T) {
	wake := make(chan string, 4)
	for _, r := range []string{"onLost", "onCapabilitiesChanged", "onLinkPropertiesChanged"} {
		select {
		case wake <- r:
		default:
			t.Fatalf("缓冲 4 的通道不该满，投递 %q 失败", r)
		}
	}
	select {
	case got := <-wake:
		if got != "onLost" {
			t.Fatalf("FIFO 期望 onLost，实际 %q", got)
		}
	default:
		t.Fatal("通道应有缓冲的 reason")
	}
}

// 并发投递不 race（Kotlin 回调线程与 Go Stop 并发）。
func TestWakeConcurrentPost(t *testing.T) {
	wake := make(chan string, 64)
	var wgSync sync.WaitGroup
	for i := 0; i < 32; i++ {
		wgSync.Add(1)
		go func(i int) {
			defer wgSync.Done()
			postWake(wake, "reason")
		}(i)
	}
	wgSync.Wait()
	if len(wake) != 32 {
		t.Fatalf("32 路并发投递应全数入缓冲，实际 %d", len(wake))
	}
}
