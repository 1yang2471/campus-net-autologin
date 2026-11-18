package app

import (
	"fmt"
	"strings"
)

// 行为确认的生产接线（A + B 一步到位）。detect.go 保持纯（不触网）：
// 真正的发包缝在组合根这一层接上守护唯一登录出口 deliverPinned，绑定源/钉
// IP/Host 纪律与守护一字不差；判定复用 keeper.JudgeLoginResponse（经
// ConfirmPortalByLogin），不另造词表逻辑。

// confirmSend 把 ConfirmOptions.Send 接到 deliverPinned——守护唯一的凭据出口。
// 安全边界：loginURL 参数被忽略，目标一律取 cfg.Server+cfg.LoginPath；绝不
// 让调用方把凭据发往任意地址（不把凭据发往猜测地址，同 routeLogin 纪律）。
// method 参数驱动 GET/POST 分派（经 cfg 副本传入 deliverPinned），与守护主线
// 同一条 routeLogin + GetPinned/PostPinned 路径。
func confirmSend(cfg daemonConfig, p *plumbing) func(string, string, map[string]string) (string, error) {
	return func(method, _ string, form map[string]string) (string, error) {
		c := cfg
		c.LoginForm.Method = strings.ToUpper(strings.TrimSpace(method))
		return deliverPinned(c, p, form)
	}
}

// ConfirmSetupPortal 首跑行为确认的生产实现：按 cfg 当前 LoginForm（字段名/
// 方法/静态参数）、Response 词表与传入凭据填表，发一次探测登录并判定。
// 凭据由调用方在面板填好后传入，只活在内存、不进 ini、不另存副本。
func ConfirmSetupPortal(cfg daemonConfig, user, pass string, p *plumbing) (bool, string, error) {
	method := strings.ToUpper(strings.TrimSpace(cfg.LoginForm.Method))
	if method == "" {
		method = "POST"
	}
	return ConfirmPortalByLogin(ConfirmOptions{
		LoginURL: cfg.Server + cfg.LoginPath,
		Method:   method,
		Form:     cfg.LoginForm.Fields(user, pass),
		Marks:    cfg.Response,
		Send:     confirmSend(cfg, p),
	})
}

// confirmSetupWithPlumbing 生产默认缝：用当前 cfg/凭据现装一份 plumbing，
// 再走 ConfirmSetupPortal（与守护同款物理源选路，绑源钉 IP 纪律不变）。
func confirmSetupWithPlumbing(cfg daemonConfig, user, pass string) (bool, string, error) {
	p, err := newPlumbing(cfg, user, pass)
	if err != nil {
		return false, "", fmt.Errorf("行为确认装配失败：%w", err)
	}
	return ConfirmSetupPortal(cfg, user, pass, p)
}

// SetupConfirmDecision 首跑向导的行为确认决策（纯函数，便于红测钉死）：
// 指纹命中直接继续；指纹未命中时跑一次行为确认——
//   - 成功：继续（与指纹成功路径同处理）；
//   - 判负：不继续，证据保证点明「词表可能不匹配，请核对 [response]」；
//   - 发包错误：不继续，证据带出系统错误根因（不伪装成判定结论）。
//
// confirm 为 nil（未接缝）时退回旧行为：不自动继续，面板允许手填。
func SetupConfirmDecision(fingerprintOK bool, confirm func() (bool, string, error)) (bool, string) {
	if fingerprintOK {
		return true, ""
	}
	if confirm == nil {
		return false, "未能自动确认门户：指纹快筛未命中且未接行为确认缝，请在面板核对 server 与 [response] 配置"
	}
	usable, ev, err := confirm()
	if err != nil {
		return false, "行为确认发包失败：" + err.Error()
	}
	if usable {
		return true, ev
	}
	ev = strings.TrimSpace(ev)
	if !strings.Contains(ev, "词表可能不匹配") {
		if ev != "" {
			ev += "；"
		}
		ev += "响应词表可能不匹配，请核对 [response] 词表后重试"
	}
	return false, ev
}
