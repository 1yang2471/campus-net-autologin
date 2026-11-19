package mobile

import (
	"strings"
	"testing"
)

// 探测入口的物理栈纪律：没有注入物理 v4 时必须当场拒绝——不猜出口、不触网。
// （安卓上 "自动发现源" 可能选到 rmnet/VPN，而不是已绑定的 Wi-Fi。）
func TestDetectPortalRequiresInjectedSource(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t) })
	_, err := DetectPortal("")
	if err == nil || !strings.Contains(err.Error(), "物理 v4 源") {
		t.Fatalf("未注入源时 DetectPortal 必须拒绝，实际 err=%v", err)
	}
}

// SetPhysicalSources 要同步探测用的钉源；清空注入时探测源必须一起清掉
// （否则会拿一个已经失效的地址去发探测请求）。
func TestSetPhysicalSourcesTracksDetectSource(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t) })
	if err := SetPhysicalSources("10.0.0.5", ""); err != nil {
		t.Fatal(err)
	}
	detectV4Mu.Lock()
	got := detectV4
	detectV4Mu.Unlock()
	if got != "10.0.0.5" {
		t.Fatalf("注入后探测源 = %q", got)
	}
	if err := SetPhysicalSources("", ""); err != nil {
		t.Fatal(err)
	}
	detectV4Mu.Lock()
	got = detectV4
	detectV4Mu.Unlock()
	if got != "" {
		t.Fatalf("清空注入后探测源应清空，实际 %q", got)
	}
}
