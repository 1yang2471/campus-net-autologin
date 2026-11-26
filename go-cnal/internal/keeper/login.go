package keeper

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"cnal/internal/dialect"
	"cnal/internal/protocol"
)

// LoginAction 守护重连登录组合根（移植 Python do_login + _dualstack_field
// 三语义）：双栈开 → 每次登录现场握手取对端 ip 携带 ipv4or6（E4 一次认证绑
// 两栈）；握手失败/应答不可解析/源不可信 → 降级纯 v4 照发（absent ≠ fail，
// 绝不挡 v4 主线）；默认关 → 零握手零字段（异校零影响）。
// 凭据只经 Deliver 缝发往用户配置的门户——构造上不进 Handshake 缝。
type LoginAction struct {
	credMu    sync.RWMutex // 保护 User/Pass（热切换 vs 并发登录）
	User      string       `json:"-"`
	Pass      string       `json:"-"`
	DualStack bool
	// Marks 响应判定词表（[response] 配置面；零值 = 默认词表）。
	// 只在启动前由组合根赋值，登录路径只读，无并发写。
	Marks ResponseMarks
	// Form 登录表单模板（ini login_form；零值 = 参考校默认）。
	// 与 Marks 同一条纪律：启动前赋值，登录路径只读。
	Form protocol.Form

	// 网络缝（测试替身；生产接线在 cmd/cnald）：
	// Handshake 绑 v6 源 POST 对端端点；无源/失败都返回 error → 降级纯 v4。
	Handshake func() (string, error)
	// Deliver 物理栈钉 IP 发登录表单，返回门户响应原文（守护主线唯一凭据出口）。
	Deliver func(form map[string]string) (string, error)
	// AfterSuccess 登录/补绑被门户接受后的非阻断后置动作；失败不得改变认证结果。
	AfterSuccess func()
}

// Relocate 热切换账号（面板切换立即生效，不重启进程）：原子换凭据，下一次
// 登录（自动或手动）即用新账号。空值拒绝且不破坏现有凭据。
func (a *LoginAction) Relocate(user, pass string) error {
	a.credMu.Lock()
	defer a.credMu.Unlock()
	if user == "" || pass == "" {
		return errors.New("账号与密码必须成对且非空")
	}
	a.User, a.Pass = user, pass
	return nil
}

// credentials 读当前凭据快照（Do 与 Relocate 并发安全）。
func (a *LoginAction) credentials() (string, string) {
	a.credMu.RLock()
	defer a.credMu.RUnlock()
	return a.User, a.Pass
}

func (a *LoginAction) notifySuccess() {
	if a.AfterSuccess != nil {
		a.AfterSuccess()
	}
}

// peerValue 现场握手取对端 v6 地址（Do 与 DoRebind 共用——同一条规矩只放一处）。
// 失败/不可解析返回 error，调用方决定降级还是拒绝。
func (a *LoginAction) peerValue() (string, error) {
	if a.Handshake == nil {
		return "", protocol.ErrPeerAbsent
	}
	resp, err := a.Handshake()
	if err != nil {
		return "", protocol.ErrPeerQuery
	}
	m, _ := dialect.Parse(resp)
	if ok, _ := m["success"].(bool); ok {
		if ip, _ := m["ip"].(string); ip != "" {
			return ip, nil
		}
	}
	return "", protocol.ErrPeerQuery
}

// V6Bind 是一次登录里 v6 绑定的实况。三态而非布尔，理由与 protocol.StackState
// 同源：「双栈没开」是正常（异校零影响），「开了却没带上」是静默降级，两者
// 塌缩成一个值就没法既保 v4 主线又把降级说出口（2026-11-09 切账号事故）。
type V6Bind string

const (
	V6Off      V6Bind = "off"      // 双栈未开：本来就不该带
	V6Sent     V6Bind = "sent"     // 带上了 ipv4or6
	V6Degraded V6Bind = "degraded" // 开了双栈却没带上（握手失败）——降级
)

// Outcome 一次认证的完整事实：门户原文（判定/展示要） + v6 绑定实况（调停
// 「v6 跟没跟着换」要）。门户原文看不出降级，只有发起方回报得了。
type Outcome struct {
	Resp string
	V6   V6Bind
}

// v6BindOf 从表单本身读绑定实况（协议侧不给第二处硬写键名的机会）。
func v6BindOf(dualStack bool, form map[string]string) V6Bind {
	if !dualStack {
		return V6Off
	}
	if protocol.HasV6Bind(form) {
		return V6Sent
	}
	return V6Degraded
}

// Do 执行一次登录：现场握手取值构造表单 → POST → 读门户应答。
// 每次登录重新走 BuildLogin（v6 临时地址会轮换，缓存会绑到失效地址）；
// 凭据取热切换快照（Relocate 后下一次登录即用新账号）。
func (a *LoginAction) Do() (Outcome, error) {
	return a.DoVia(a.Deliver)
}

// DoVia 用调用方给定的投递端执行一次登录：表单构造与应答判定与 Do 完全一致，
// 只有「凭据发往哪条出口」不同。
//
// 双通道用这条缝（2026-11-20 M2）：门户按**来源 IP** 记会话，通道登录必须
// 绑到那条通道自己的源 IP——用主链路的出口发，认证的是主链路那条会话，
// 通道照样是未认证的。共用 DoVia 而不是复制一份，是为了「表单长什么样、
// 成功怎么判」只有一处实现（复制一份迟早漂移）。
func (a *LoginAction) DoVia(deliver func(form map[string]string) (string, error)) (Outcome, error) {
	user, pass := a.credentials()
	peer := protocol.NewPeerProvider(func() (string, error) { return a.peerValue() })
	form := protocol.BuildLogin(user, pass, protocol.Settings{DualStack: a.DualStack, Form: a.Form}, peer)
	bind := v6BindOf(a.DualStack, form)
	resp, err := deliver(form)
	if err != nil {
		return Outcome{V6: bind}, err
	}
	judged, jerr := JudgeLoginResponse(resp, a.Marks.withDefaults())
	if jerr == nil {
		a.notifySuccess()
	}
	return Outcome{Resp: judged, V6: bind}, jerr
}

// DoBind 用调用方给定的 v6 地址做一次完整认证。切账号时本机可能同时有
// 稳定地址与隐私临时地址两个公网 v6 会话（门户按地址记在线），只搬其中一个
// 会出现「v4 换了、IPv6 仍是原账号」；每个地址各绑一次才搬干净。
// 与 Do 的差别只在 ipv4or6 的来源：地址由调用方枚举，不再现场握手。
func (a *LoginAction) DoBind(v6 string) (Outcome, error) {
	if !a.DualStack {
		return Outcome{V6: V6Off}, errors.New("双栈未开启，无法按地址绑定 IPv6")
	}
	user, pass := a.credentials()
	form := protocol.BuildLogin(user, pass, protocol.Settings{DualStack: true, Form: a.Form},
		protocol.NewPeerProvider(func() (string, error) { return v6, nil }))
	resp, err := a.Deliver(form)
	if err != nil {
		return Outcome{V6: V6Sent}, err
	}
	judged, jerr := JudgeLoginResponse(resp, a.Marks.withDefaults())
	if jerr == nil {
		a.notifySuccess()
	}
	return Outcome{Resp: judged, V6: V6Sent}, jerr
}

// DoRebind 主动补绑 v6：与 Do 的差别是「握手失败必须拒绝」——Do 降级纯 v4
// 是为了保 v4 主线（正确），但补绑的唯一目的就是带上 ipv4or6，降级等于
// 发了个没有 v6 的假补绑还报告成功。双栈关闭时同样拒绝（无绑定语义）。
// 2026-11-08 实证：v4 长期在线 + 免认证时段禁自动登录 → 用户看到的
// 「IPv6 未在线（可补绑）」永远没有触发点，此方法是那个触发点。
func (a *LoginAction) DoRebind() error {
	if !a.DualStack {
		return errors.New("双栈未开启，无法补绑 IPv6")
	}
	ip, err := a.peerValue()
	if err != nil {
		return fmt.Errorf("补绑失败：现场握手取不到 v6 地址（%v）", err)
	}
	user, pass := a.credentials()
	form := protocol.BuildLogin(user, pass, protocol.Settings{DualStack: true, Form: a.Form},
		protocol.NewPeerProvider(func() (string, error) { return ip, nil }))
	resp, err := a.Deliver(form)
	if err != nil {
		return err
	}
	if _, err := JudgeLoginResponse(resp, a.Marks.withDefaults()); err != nil {
		// 补绑语境只留一个前缀（判定器的「登录未受理」不叠加）；
		// 面板/托盘直接显示这句，门户原文才是用户要的失败原因。
		return fmt.Errorf("门户未受理补绑: %s", strings.TrimSpace(resp))
	}
	a.notifySuccess()
	return nil
}
