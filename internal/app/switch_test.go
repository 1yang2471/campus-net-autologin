package app

import (
	"errors"
	"net"
	"strings"
	"testing"

	"cnal/internal/keeper"
)

// 2026-11-09 两次踩坑，两次都是「动词只做了一半」：
//   上午：面板「切换账号」只换凭据（Relocate）不认证，文案却写「即时生效」；
//   下午：补了「切换即认证」之后，v4 换了、v6 仍挂旧账号——门户按地址记在线，
//         地址已挂在别人名下时登录里的 ipv4or6 改不了归属（实测：
//         v4=新账号 / v6=旧账号，用门户 online_check 读出来的）。
// 所以现在的契约是三步：换凭据 → 释放旧 v6 栈 → 认证 → 核对归属。

// 替身：按调用顺序记账，默认 release/inspect 都成功。
func opsFor(t *testing.T, seq *[]string, out keeper.Outcome, v6User, v4User string) sessionOps {
	t.Helper()
	return sessionOps{
		releaseV6: func() v6Release {
			*seq = append(*seq, "release-v6")
			return v6Release{}
		},
		releaseV4: func() error {
			*seq = append(*seq, "release-v4")
			return nil
		},
		authenticate: func() (keeper.Outcome, error) {
			*seq = append(*seq, "auth")
			return out, nil
		},
		inspect: func() stackOwners {
			*seq = append(*seq, "inspect")
			return stackOwners{V4Known: true, V4User: v4User, V6Known: true, V6User: v6User}
		},
	}
}

// 行为：顺序必须是「先释放旧栈，再认证」——反了就是白登一次（地址还在旧账号名下）。
// v4 也必须在认证前注销：门户按地址记在线，v4 还在旧账号时新登录只会答「已在线」，
// 归属不会改（2026-11-10 流量用完后切账号失败实证）。v4 注销放最后，尽量晚断网。
func TestSwitchAccountReleasesBeforeAuthenticating(t *testing.T) {
	var seq []string
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil },
		opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new"))
	if err != nil {
		t.Fatalf("切换应成功: %v", err)
	}
	want := []string{"release-v6", "release-v4", "auth", "inspect"}
	if strings.Join(seq, ",") != strings.Join(want, ",") {
		t.Errorf("调用顺序应是 %v，实际 %v", want, seq)
	}
	if !strings.Contains(msg, "已切换到 new") {
		t.Errorf("回报必须点名账号，实际 %q", msg)
	}
}

// 行为：v4 仍归旧账号 = 切换没生效。必须明确说「未生效」并点名旧账号，
// 不得被 v6 不在线/不可观测掩盖，也不得建议一个走不通的动作。
func TestSwitchAccountWarnsWhenV4StillOnOldAccount(t *testing.T) {
	var seq []string
	msg, _ := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil },
		opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "old"))
	if !strings.Contains(msg, "切换未生效") || !strings.Contains(msg, "v4 仍归 old") {
		t.Errorf("v4 没换人必须明确报「切换未生效」并点名旧账号，实际 %q", msg)
	}
	if strings.Contains(msg, "都归") {
		t.Errorf("v4 未跟随时不得声称两栈都归新账号，实际 %q", msg)
	}
}

// 行为：注销后新登录没建立 v4 会话（门户 online:0）同样是失败，不能含糊成成功。
func TestSwitchAccountReportsV4Offline(t *testing.T) {
	var seq []string
	msg, _ := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil },
		opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "", ""))
	if !strings.Contains(msg, "v4 当前不在线") {
		t.Errorf("v4 未建立会话必须如实说，实际 %q", msg)
	}
}

// 行为：门户确认两栈都归新账号 → 回报直接说「都归」，不含糊。
func TestSwitchAccountReportsBothStacksOwned(t *testing.T) {
	var seq []string
	msg, _ := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil },
		opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new"))
	if !strings.Contains(msg, "门户确认 v4/v6 都归 new") {
		t.Errorf("两栈一致时应明说，实际 %q", msg)
	}
}

// 行为：v6 仍归旧账号 → 必须点名旧账号并给出下一步；这是本次修复的验收线。
func TestSwitchAccountWarnsWhenV6StillOnOldAccount(t *testing.T) {
	var seq []string
	msg, _ := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil },
		opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "old", "new"))
	if !strings.Contains(msg, "仍归 old") || !strings.Contains(msg, "未跟随") {
		t.Errorf("v6 没跟过去必须点名旧账号并给下一步，实际 %q", msg)
	}
}

// 行为：归属核对读不到（门户页/超时）→ 如实说"没确认"，不许当作已跟随。
func TestSwitchAccountReportsInspectFailure(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	ops.inspect = func() stackOwners {
		return stackOwners{V4Err: errors.New("online_check 未受理"), V6Err: errors.New("online_check 未受理")}
	}
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if err != nil {
		t.Fatalf("核对失败不该让切换报错（凭据与认证都已成功）: %v", err)
	}
	if !strings.Contains(msg, "未能确认 v4") {
		t.Errorf("核对失败必须说「没确认」，实际 %q", msg)
	}
}

// 行为：释放失败不阻断切换（认证照做），但要在回报里说清楚是哪一侧失败。
func TestSwitchAccountReportsReleaseFailureButStillAuthenticates(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	ops.releaseV4 = func() error {
		seq = append(seq, "release-v4")
		return errors.New("logout 超时")
	}
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if err != nil {
		t.Fatalf("释放失败不该让切换报错: %v", err)
	}
	if !strings.Contains(strings.Join(seq, ","), "release-v6,release-v4,auth") {
		t.Errorf("释放失败后仍必须认证，实际顺序 %v", seq)
	}
	if !strings.Contains(msg, "v4 注销失败") {
		t.Errorf("v4 释放失败必须写进回报，实际 %q", msg)
	}
}

// 行为：keyring 里没有该账号密码 → 拒绝，且不换凭据、不释放、不认证、不核对。
func TestSwitchAccountRejectsMissingPassword(t *testing.T) {
	for _, load := range []func(string) (string, error){
		func(string) (string, error) { return "", errors.New("no such credential") },
		func(string) (string, error) { return "", nil }, // 空密码同样拒绝
	} {
		var seq []string
		_, err := switchAccount("new", load,
			func(string, string) error { seq = append(seq, "relocate"); return nil },
			sessionOps{
				releaseV6:    func() v6Release { seq = append(seq, "release-v6"); return v6Release{} },
				releaseV4:    func() error { seq = append(seq, "release-v4"); return nil },
				authenticate: func() (keeper.Outcome, error) { seq = append(seq, "auth"); return keeper.Outcome{}, nil },
				inspect:      func() stackOwners { seq = append(seq, "inspect"); return stackOwners{} },
			})
		if err == nil {
			t.Fatal("取不到密码必须报错，不能假装切换成功")
		}
		if len(seq) != 0 {
			t.Errorf("密码缺失时不得做任何动作，实际 %v", seq)
		}
	}
}

// 行为：双栈没开（V6Off）是正常形态，不该出现"未跟随/释放"这类告警。
// 此时 v6 不参与切换，归属核对也不应因「无 v6」报不可观测。
func TestSwitchAccountDoesNotWarnWhenDualStackOff(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Off}, "new", "new")
	ops.inspect = func() stackOwners {
		return stackOwners{V4Known: true, V4User: "new"} // 生产：双栈关时跳过 v6 核对
	}
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if err != nil {
		t.Fatalf("切换应成功: %v", err)
	}
	if !strings.Contains(msg, "双栈未开") {
		t.Errorf("应说明只搬了 v4，实际 %q", msg)
	}
	if strings.Contains(msg, "未跟随") || strings.Contains(msg, "释放失败") || strings.Contains(msg, "不可观测") {
		t.Errorf("双栈未开不是故障，不该出现告警措辞，实际 %q", msg)
	}
}

// 行为：认证被门户拒绝（如冻结）不改变"凭据已切换"，所以不报错但要透传原文。
func TestSwitchAccountReportsPortalRejectionWithoutClaimingFailure(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	ops.authenticate = func() (keeper.Outcome, error) {
		return keeper.Outcome{V6: keeper.V6Sent},
			errors.New("登录未受理: 用户被冻结，冻结剩余60s")
	}
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if err != nil {
		t.Fatalf("凭据已保存，切换本身不该报错（否则面板会写「切换失败」而误导）: %v", err)
	}
	if !strings.Contains(msg, "冻结剩余60s") {
		t.Errorf("必须透传门户原文，实际 %q", msg)
	}
	if !strings.Contains(msg, "可能离线") {
		t.Errorf("v4 已注销而认证失败时必须提醒可能离线，实际 %q", msg)
	}
}

// 2026-11-11 实证：本机同时有稳定 v6 与 Windows 隐私临时地址，门户按**地址**
// 记在线。切账号只搬主地址 → v4 换了、IPv6 仍是原账号（online_check 逐地址
// 读出：稳定归新账号、临时仍归旧账号）。行为：切换前在线、且已注销成功的
// 其它本地 v6，必须在主认证之后逐地址重绑到新账号。
func TestSwitchAccountBindsExtraV6Sessions(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	temp := net.ParseIP("2001:db8:1::99")
	ops.releaseV6 = func() v6Release {
		seq = append(seq, "release-v6")
		return v6Release{Extras: []v6Session{{Addr: temp, WasOnline: true}}}
	}
	ops.bindV6 = func(ip net.IP) error {
		seq = append(seq, "bind-v6:"+ip.String())
		return nil
	}
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if err != nil {
		t.Fatalf("切换应成功: %v", err)
	}
	want := []string{"release-v6", "release-v4", "auth", "bind-v6:" + temp.String(), "inspect"}
	if strings.Join(seq, ",") != strings.Join(want, ",") {
		t.Errorf("额外 v6 必须在主认证之后、核对之前重绑；期望 %v，实际 %v", want, seq)
	}
	if !strings.Contains(msg, temp.String()) || !strings.Contains(msg, "已随新账号绑定") {
		t.Errorf("回报必须点名额外 v6 地址与结果，实际 %q", msg)
	}
}

// 行为：额外 v6 切换前不在线 → 无需重绑（不浪费登录、不撞门户限频）。
func TestSwitchAccountSkipsOfflineExtraV6(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	ops.releaseV6 = func() v6Release {
		return v6Release{Extras: []v6Session{{Addr: net.ParseIP("2001:db8::2")}}}
	}
	ops.bindV6 = func(ip net.IP) error {
		seq = append(seq, "bind-v6")
		return nil
	}
	if _, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops); err != nil {
		t.Fatal(err)
	}
	for _, s := range seq {
		if strings.HasPrefix(s, "bind-v6") {
			t.Errorf("离线地址不该重绑，实际 %v", seq)
		}
	}
}

// 行为：额外 v6 旧会话注销失败 → 不得假装已绑定；回报点名地址与原因。
func TestSwitchAccountReportsExtraV6ReleaseFailure(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	addr := net.ParseIP("2001:db8::3")
	ops.releaseV6 = func() v6Release {
		return v6Release{Extras: []v6Session{{Addr: addr, WasOnline: true,
			ReleaseErr: errors.New("logout 超时")}}}
	}
	bindCalled := false
	ops.bindV6 = func(net.IP) error { bindCalled = true; return nil }
	msg, _ := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if bindCalled {
		t.Error("旧会话没注销成功就不能绑新账号（发货不对地址）")
	}
	if !strings.Contains(msg, addr.String()) || !strings.Contains(msg, "未能注销") {
		t.Errorf("回报必须点名地址与注销失败，实际 %q", msg)
	}
}

// 行为：额外 v6 重绑失败 → 切换本身仍成功（凭据已落地），但必须如实说哪个
// 地址没绑上，不能吞掉。
func TestSwitchAccountReportsExtraV6BindFailure(t *testing.T) {
	var seq []string
	ops := opsFor(t, &seq, keeper.Outcome{V6: keeper.V6Sent}, "new", "new")
	addr := net.ParseIP("2001:db8::4")
	ops.releaseV6 = func() v6Release {
		return v6Release{Extras: []v6Session{{Addr: addr, WasOnline: true}}}
	}
	ops.bindV6 = func(net.IP) error { return errors.New("门户未受理") }
	msg, err := switchAccount("new",
		func(string) (string, error) { return "pw", nil },
		func(string, string) error { return nil }, ops)
	if err != nil {
		t.Fatalf("单个地址绑失败不该让切换报错: %v", err)
	}
	if !strings.Contains(msg, addr.String()) || !strings.Contains(msg, "未能绑到新账号") {
		t.Errorf("必须点名失败地址，实际 %q", msg)
	}
}
