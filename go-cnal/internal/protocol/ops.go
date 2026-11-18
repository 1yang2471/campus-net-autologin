package protocol

import (
	"errors"
	"fmt"
	"strings"

	"cnal/internal/dialect"
)

// 按栈会话 op。词表是校事实，2026-11-09 从门户自己的 logic_new.js 读出（不是猜的）：
//
//	online_check —— 只读：这一栈现在挂在哪个账号下（响应 online + username）
//	logout       —— 按栈注销：{opr:logout, ipv4or6:<该栈地址>}
//
// 两者都不带凭据：门户按请求源地址认客户端，与 ipv4or6online 同一条纪律
// （peer.go 的 HandshakeForm 也是"构造上没有凭据参数"）。
const (
	OpOnlineCheck = "online_check"
	OpLogout      = "logout"
)

// OnlineState 是 online_check 的回答：这一栈在不在线、归谁。
//
// 存在的理由：登录时带的 ipv4or6 只能收编**还没在线**的 v6；地址若已挂在
// 别的账号下，门户不会改归属。而 v6 四态只问"在不在线"，从不问"归谁"——
// 于是"v4 换了新账号、v6 还在旧账号"这件事对用户完全隐形（2026-11-09 实测）。
type OnlineState struct {
	Online   bool
	Username string
}

// OpForm 构造按栈 op 的表单。地址为空就不带 ipv4or6（门户页面在纯 v4 上就是空串）。
func OpForm(opr, ipv4or6 string) map[string]string {
	f := map[string]string{"opr": opr}
	if ipv4or6 != "" {
		f["ipv4or6"] = ipv4or6
	}
	return f
}

// ParseOnlineCheck 解析 online_check 应答。**读不懂一律报错，不猜"不在线"**：
// 猜成不在线会让调用方去补绑，而真实情况可能只是响应被劫持或读不懂。
func ParseOnlineCheck(raw string) (OnlineState, error) {
	m, err := dialect.Parse(raw)
	if err != nil {
		return OnlineState{}, fmt.Errorf("online_check 应答不可解析: %w", err)
	}
	if ok, _ := m["success"].(bool); !ok {
		return OnlineState{}, errors.New("online_check 未受理（可能是门户页或劫持页）")
	}
	online, present := truthy(m["online"])
	if !present {
		return OnlineState{}, errors.New("online_check 应答缺 online 字段")
	}
	if !online {
		return OnlineState{Online: false}, nil
	}
	user, _ := m["username"].(string)
	return OnlineState{Online: true, Username: strings.TrimSpace(user)}, nil
}

// truthy 把门户的布尔/数字/字符串三形态读成 bool——真实响应里 online 是数字 1。
func truthy(v any) (value bool, known bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case float64:
		return t != 0, true
	case int:
		return t != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off", "":
			return false, true
		}
	}
	return false, false
}
