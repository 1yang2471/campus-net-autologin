package app

import (
	"errors"
	"fmt"
	"net"
	"time"

	"cnal/internal/keeper"
	"cnal/internal/netstack"
)

// 通道级登录的投递端（2026-11-20 双通道方案 §8.3 M2）。
//
// 门户按**来源 IP** 记会话（11-20 实测：WiFi 已认证不放行有线口 IP，有线
// 必须从有线源再登一次）。所以通道登录必须：
//   - 绑定**该通道自己的源 IP**（物理栈纪律：绕开 TUN，不走系统栈）；
//   - 钉住门户真实 IP（校园 DNS 绑源解析的结果）——直连域名会拿到 fake-ip。
//
// 凭据只发往用户配置的门户（server），host 指纹纪律与主链路同款。

// channelDeliver 造通道登录的投递端：绑 channelSrc 发往 portalIP。
//
// 参数校验（返回错误、不投递）：
//   - channelSrc 非法 → 拒（凭据不许发往未知出口）；
//   - portalIP 未知 → 拒。
//
// 「解析源==通道源」不在投递端校验：它由 channelLogin 的 ResolveBound(src,
// ...) 结构性保证（解析绑的就是通道源）。旧版把 portalIP 误当「解析源」与
// channelSrc 全等校验——真机上门户 IP(1.1.1.2) 永远 ≠ 通道源(172.29.153.208)，
// 通道登录 100% 恒拒（2026-11-21 M2-c 真机踢线实证：WiFi 掉线后通道无人自愈
// 的根因；单测用同值 127.0.0.1 掩盖了它）。
func channelDeliver(p *plumbing, cfg daemonConfig, channelSrc, portalIP net.IP, portalPort string) func(map[string]string) (string, error) {
	return func(form map[string]string) (string, error) {
		if channelSrc == nil || channelSrc.To4() == nil {
			return "", errors.New("通道源地址非法：不投递凭据")
		}
		if portalIP == nil || portalIP.To4() == nil {
			return "", errors.New("门户 IP 未知：不投递凭据")
		}
		client := netstack.NewWithBindMode(channelSrc, cfg.LoginTimeout, p.bindMode)
		if !cfg.SSLVerify {
			client = client.InsecureTLS()
		}
		pin := net.JoinHostPort(portalIP.String(), portalPort)
		headers := map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"User-Agent":   cfg.UserAgent,
			"Referer":      cfg.Server + cfg.LoginPage,
		}
		// GET 型登录（异校明文族）：参数走 query、body 空（同 deliverPinned）。
		if cfg.LoginForm.Method == "GET" {
			return client.GetPinned(cfg.Server+cfg.LoginPath, form,
				map[string]string{"User-Agent": cfg.UserAgent, "Referer": cfg.Server + cfg.LoginPage},
				pin)
		}
		return client.PostPinned(cfg.Server+cfg.LoginPath, form, headers, pin)
	}
}

// channelLogin 对指定通道发起一次登录（完整动作：解析门户 IP → 投递 → 判定）。
//
// 速率闸门在这里（app 层 RateLimit 是**账号级单例**，主链路与通道共用）：
// 通道登录消耗的是同一份额度——门户的反暴力冻结是账号级的。
func (p *plumbing) channelLogin(cfg daemonConfig, ch ChannelRef) (string, error) {
	src := net.ParseIP(ch.IP)
	if src == nil || src.To4() == nil {
		return "", errors.New("通道源地址非法")
	}
	// 门户真实 IP：绑**该通道的源**解析（不能用主链路的解析结果——那是
	// 另一条链路上的 IP，拨过去认证的不是这条通道）。
	ip, err := netstack.ResolveBoundWithBindMode(src, cfg.CampusDNS, p.portalHost, cfg.ProbeTimeout, p.bindMode)
	if err != nil || ip.To4() == nil {
		return "", fmt.Errorf("绑源解析门户失败: %w", err)
	}
	// 速率闸门先行（与主链路同款：登录**前**判，登录后记账会多放行一次）。
	// p.rl 是账号级单例——通道登录与主链路共用同一份额度。
	if p.rl != nil {
		if !p.rl.AllowAutoLogin() {
			return "", fmt.Errorf("%w：滚动 1 小时自动登录已达上限，闸门关闭 15 分钟", keeper.ErrRateCapped)
		}
	}
	deliver := channelDeliver(p, cfg, src, ip, p.portalPort)
	out, err := p.action.DoVia(deliver)
	if err != nil {
		return "", err
	}
	if p.rl != nil {
		p.rl.RecordSuccess(time.Now())
	}
	return out.Resp, nil
}

// ChannelRef 通道引用（app 层用的最小形态：IP + 标识）。
type ChannelRef struct {
	ID    string
	Iface string
	IP    string
}
