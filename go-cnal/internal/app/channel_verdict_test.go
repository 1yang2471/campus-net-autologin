package app

import (
	"testing"

	"cnal/internal/keeper"
)

// 通道认证态四态（2026-11-20 双通道方案 §8.3 M1）。
//
// 四态比旧的 siblingVerdict 三态多切出一档，因为现场有两种完全不同的
// 「不通」，处理方式也不同：
//
//   - blackholed：DNS 解析成功但 TCP 超时——未认证 VLAN 的典型形态
//     （11-19 实测：以太网 443 全 refused、NCSI 302 劫持到门户）；
//     走门户登录是可行的，但**必须先能到门户**，否则登录请求本身发不出去。
//   - offline：连 DNS 都解析不出来 / 网卡不可达——这条通道根本没在网，
//     任何登录尝试都是浪费预算，必须跳过。
//
// 判定结构仍沿用 siblingVerdict 的形状（3xx 看 Location、2xx 看门户指纹），
// 差别只在「不通」的归类与「能否登录」的结论。

// 行为：3xx 指向门户 = 未认证（可登录）；3xx 指向别处 = 已认证。
func TestChannelVerdictUnauthVsAuthed(t *testing.T) {
	marks := []string{"ac_portal", "pwdLogin"}
	if got := channelVerdict(302, "https://portal.example.edu/ac_portal/proxy.html", "", "portal.example.edu", marks); got != keeper.ChannelUnauth {
		t.Errorf("302 指向门户应判未认证，实际 %s", got)
	}
	if got := channelVerdict(302, "https://www.baidu.com/", "", "portal.example.edu", marks); got != keeper.ChannelAuthed {
		t.Errorf("302 指向别处应判已认证，实际 %s", got)
	}
}

// 行为：2xx 带门户指纹 = 未认证（200 型劫持页）；2xx 普通内容 = 已认证。
func TestChannelVerdict200Hijack(t *testing.T) {
	marks := []string{"ac_portal", "pwdLogin"}
	if got := channelVerdict(200, "", "<html>ac_portal</html>", "portal.example.edu", marks); got != keeper.ChannelUnauth {
		t.Errorf("2xx 带门户指纹应判未认证，实际 %s", got)
	}
	if got := channelVerdict(200, "", "<html>hi</html>", "portal.example.edu", marks); got != keeper.ChannelAuthed {
		t.Errorf("2xx 普通内容应判已认证，实际 %s", got)
	}
}

// 行为：请求没发出去（超时/拒绝）= 黑洞——与「已认证」严格区分，不得误报在线。
func TestChannelVerdictBlackholeNotAuthed(t *testing.T) {
	marks := []string{"ac_portal"}
	if got := channelVerdict(0, "", "", "portal.example.edu", marks); got != keeper.ChannelBlackholed {
		t.Errorf("请求未发出应判黑洞，实际 %s", got)
	}
}

// 行为：其它状态码（如 500）= 不可判定——不得当成未认证（那会触发无谓登录）。
func TestChannelVerdictUnknownForOtherStatus(t *testing.T) {
	marks := []string{"ac_portal"}
	if got := channelVerdict(500, "", "", "portal.example.edu", marks); got != keeper.ChannelOffline {
		t.Errorf("500 应判不可判定（offline），实际 %s", got)
	}
}

// 行为：DNS 解析失败 = offline（这条通道不在网），与黑洞严格分开。
func TestChannelStateFromDNSErr(t *testing.T) {
	st := channelState(dnsFailed, "dial", "portal.example.edu", []string{"ac_portal"})
	if st != keeper.ChannelOffline {
		t.Errorf("DNS 失败应判离线，实际 %s", st)
	}
}

// 行为：黑洞态不可登录（登录请求同样发不出去），未认证态可登录。
func TestChannelLoginEligibility(t *testing.T) {
	if keeper.ChannelLoginable(keeper.ChannelBlackholed) {
		t.Error("黑洞态不得判可登录")
	}
	if keeper.ChannelLoginable(keeper.ChannelOffline) {
		t.Error("离线态不得判可登录")
	}
	if !keeper.ChannelLoginable(keeper.ChannelUnauth) {
		t.Error("未认证态应可登录")
	}
}
