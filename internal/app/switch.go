package app

import (
	"fmt"
	"net"

	"cnal/internal/keeper"
)

// v6Session 一个本地公网 v6 地址的切换现场：切换前是否在线、注销是否成功。
// 门户按**地址**记在线，同接口的稳定地址与隐私临时地址是两个独立会话。
type v6Session struct {
	Addr       net.IP
	WasOnline  bool
	ReleaseErr error
}

// v6Release 主 v6 栈的注销结果 + 额外地址现场。主地址由 authenticate 统一
// 重绑；Extras 里切换前在线的地址要在主认证之后各绑一次。
type v6Release struct {
	Err    error
	Extras []v6Session
}

// sessionOps 是「切账号」需要的网络动作（组合根注入，测试用替身）。
// 拆成结构体而不是自由参数：这几件事是同一个会话操作的几个面，
// 调用方一次交齐，接口面只多一个类型。
type sessionOps struct {
	// releaseV6 注销旧 v6 栈。v6 的归属不会被新账号的登录改写（地址已在线时
	// 门户不换人），所以必须显式注销——这是 2026-11-09 实测出来的机制。
	// 2026-11-11 补：同接口的隐私临时地址也是独立会话，一并释放并记为
	// Extras（只搬主地址会留下「v4 换了、IPv6 仍是原账号」）。
	// 双栈未开时组合根传空结果（v6 不归本次切换管）。
	releaseV6 func() v6Release
	// releaseV4 注销旧 v4 栈。2026-11-10 事故推翻了「v4 会被下一次登录自然
	// 改写」的旧假设：v4 还挂在旧账号（流量用完、会话仍在）时，新登录只会
	// 得到「用户已在线，不需要再次认证」——判定器把它当成功哨兵，归属却不改。
	// 必须在认证前显式注销，与门户自己的「先注销再登录」流程同形。
	releaseV4 func() error
	// authenticate 用（已切换的）凭据做一次完整认证（主 v4/v6 地址）。
	authenticate func() (keeper.Outcome, error)
	// bindV6 用额外本地 v6 地址各认证一次（多地址切换；nil = 不处理）。
	bindV6 func(addr net.IP) error
	// inspect 只读核对 v4/v6 各挂在谁名下（两侧独立，v6 不可观测不掩盖 v4）。
	inspect func() stackOwners
}

// stackOwners 是门户对两栈归属的读数。V4 承重：v4 没换人就是切换没生效；
// v6 不可观测只影响 v6 的提示，绝不能把 v4 的结论一起吞掉（2026-11-10 事故）。
type stackOwners struct {
	V4User  string // 空 = 门户说 v4 不在线
	V4Known bool
	V4Err   error
	V6User  string // 空 = 门户说 v6 不在线
	V6Known bool
	V6Err   error
}

// switchAccount 是「切换账号」的唯一实现：换凭据 → 释放旧 v6/v4 栈 → 立即认证
// → 核对归属 → 把结果如实说成人话。
//
// 三次踩坑的教训都写在这里：
//   - 2026-11-09 上午：原先只做 Relocate 就回报「即时生效」，其实什么都没认证；
//   - 2026-11-09 下午：补了"切换即认证"之后，v4 换了、v6 仍挂旧账号——因为
//     门户按**地址**记在线，地址已挂在别人名下时，登录里的 ipv4or6 不会改归属。
//     真正能让 v6 跟过去的是按栈注销（门户自己的 logout op），不是再多登一次。
//   - 2026-11-10 晚上：流量用完后切账号，v4 也没换——旧账号会话仍在线，新登录
//     被答「用户已在线」并按成功哨兵放过。v4 同样必须先注销，不能再假设
//     "下一次登录会改写归属"。
//
// 依赖以函数注入，是为了能在不碰 keyring/ini 的前提下测顺序与回报措辞。
func switchAccount(user string,
	loadPass func(user string) (string, error),
	relocate func(user, pass string) error,
	ops sessionOps) (string, error) {
	pass, err := loadPass(user)
	if err != nil || pass == "" {
		return "", fmt.Errorf("凭据管理器无 %q 的密码，切换被拒绝", user)
	}
	if err := relocate(user, pass); err != nil {
		return "", err
	}

	// 顺序不能反：认证想收编地址，而地址此刻还挂在旧账号下——先认证等于白登。
	// v6 先、v4 后：v4 注销会短暂断掉数据通道，放到认证前一刻，尽量少断网。
	v6 := ops.releaseV6()
	releaseErrV4 := ops.releaseV4()
	out, authErr := ops.authenticate()
	if authErr != nil {
		// 凭据已落地：认证失败是门户的事（冻结/密码错），不是「切换失败」。
		// 但 v4 旧会话已在认证前注销，必须提醒用户当前可能离线，别静默丢网。
		msg := fmt.Sprintf("已切换到 %s（凭据已保存），但认证未成功：%v", user, authErr)
		if releaseErrV4 == nil {
			msg += "；旧 v4 会话已注销，当前可能离线——修正凭据后重试，或切回原账号"
		} else {
			msg += fmt.Sprintf("；v4 注销失败（%v）", releaseErrV4)
		}
		if v6ReleasedAny(v6) {
			msg += "；其它本机 v6 会话也已注销，这些地址当前同样离线"
		}
		return msg, nil
	}

	msg := switchReport(user, out.V6)
	// 额外 v6 地址：主认证只带得动主地址。切换前在线的其余本地 v6（Windows
	// 隐私临时地址）必须各绑一次，否则门户上它仍归旧账号——用户看到的就是
	// 「IPv4 换了，IPv6 还是原账号」。
	if ops.bindV6 != nil {
		for _, s := range v6.Extras {
			if !s.WasOnline {
				continue // 切换前不在线：没有会话要搬
			}
			if s.ReleaseErr != nil {
				msg += fmt.Sprintf("；v6 地址 %s 旧会话未能注销（%v），未绑定新账号", s.Addr, s.ReleaseErr)
				continue
			}
			if err := ops.bindV6(s.Addr); err != nil {
				msg += fmt.Sprintf("；v6 地址 %s 未能绑到新账号（%v）", s.Addr, err)
			} else {
				msg += fmt.Sprintf("；v6 地址 %s 已随新账号绑定", s.Addr)
			}
		}
	}
	// 核对归属：「到底跟没跟过去」只能由门户说，不能由我们推断。v4 是承重结论，
	// v6 不可观测不得把它掩盖（2026-11-10 事故的隐形根源）。
	msg += ownerClause(user, ops.inspect())
	if v6.Err != nil {
		msg += fmt.Sprintf("；旧 v6 栈释放失败（%v）", v6.Err)
	}
	if releaseErrV4 != nil {
		msg += fmt.Sprintf("；v4 注销失败（%v）", releaseErrV4)
	}
	return msg, nil
}

// v6ReleasedAny 是否真的注销过额外 v6 会话（认证失败时提醒用户断网范围用）。
func v6ReleasedAny(v v6Release) bool {
	for _, s := range v.Extras {
		if s.WasOnline && s.ReleaseErr == nil {
			return true
		}
	}
	return false
}

// switchReport 把 v6 绑定实况说成用户看得懂的一句话（措辞只在这里出现一次）。
func switchReport(user string, bind keeper.V6Bind) string {
	switch bind {
	case keeper.V6Sent:
		return fmt.Sprintf("已切换到 %s，并已用新账号认证一次（v4/v6 一起搬）", user)
	case keeper.V6Degraded:
		return fmt.Sprintf("已切换到 %s，认证未携带 ipv4or6", user)
	default:
		return fmt.Sprintf("已切换到 %s，并已用新账号认证一次（双栈未开，仅 v4）", user)
	}
}

// ownerClause 把「门户说这两栈归谁」翻成一句话。v4 是切换是否生效的主判据：
// v4 仍归旧账号/未建立会话都必须明确说「未生效」，绝不用 v6 的状态掩盖。
func ownerClause(want string, o stackOwners) string {
	if !o.V4Known {
		if o.V4Err != nil {
			return fmt.Sprintf("；未能确认 v4 归属（%v），无法确认切换是否生效", o.V4Err)
		}
		return ""
	}
	switch {
	case o.V4User == "":
		return "；但门户显示 v4 当前不在线——新账号的 v4 会话未建立"
	case o.V4User != want:
		return fmt.Sprintf("；但门户显示 v4 仍归 %s——切换未生效（旧会话未被门户释放）。"+
			"可再点一次「切换账号」；仍不行需在门户页面手动注销后重登", o.V4User)
	}
	// v4 已归新账号：再报 v6（不可观测/不在线都只是补充信息）。
	if !o.V6Known {
		if o.V6Err != nil {
			return fmt.Sprintf("；门户确认 v4 已归 %s（v6 归属不可观测：%v）", want, o.V6Err)
		}
		return "；门户确认 v4 已归 " + want
	}
	switch {
	case o.V6User == "":
		return fmt.Sprintf("；门户确认 v4 已归 %s（v6 当前不在线）", want)
	case o.V6User == want:
		return fmt.Sprintf("；门户确认 v4/v6 都归 %s", want)
	default:
		return fmt.Sprintf("；门户确认 v4 已归 %s，但 v6 仍归 %s——v6 未跟随（可再点一次「切换账号」重试）",
			want, o.V6User)
	}
}
