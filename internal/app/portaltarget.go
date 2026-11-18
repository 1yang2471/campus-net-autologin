package app

import (
	"errors"
	"net"
	"sync"
	"time"
)

// portalTarget 门户真实 IP 的 TTL 缓存（Python keeper._portal_target 同源）：
// TUN 劫持系统 DNS 给 fake-ip（2026-11-08 真机实证 a.stu.edu.cn → 28.0.0.17），
// 绑物理源发往 fake-ip 必然超时——登录/补绑/flux 必须钉校园 DNS 绑源解析出的
// 真实 IP。解析失败返回 nil：调用方跳过该拍，绝不把凭据发往猜测地址
// （Python P1-13：旧版硬编码 10.0.0.1 兜底会把学号密码送往公网）。
//
// 缓存 TTL：门户 IP 不常变，每拍探测都打 DNS 既慢又吵；休眠唤醒/换网后
// TTL 到期自然重解析（与物理源重发现互补，不引入第二套失效逻辑）。
type portalTarget struct {
	mu      sync.Mutex
	resolve func() (net.IP, error)
	ttl     time.Duration
	ip      net.IP
	at      time.Time
	now     func() time.Time
}

// newPortalTarget 构造；resolve 是生产解析缝（绑源校园 DNS），测试可注入替身。
func newPortalTarget(resolve func() (net.IP, error), ttl time.Duration) *portalTarget {
	return &portalTarget{resolve: resolve, ttl: ttl, now: time.Now}
}

// invalidate 强制下一次 get 重新解析（物理源重发现/换网后调用）。
func (p *portalTarget) invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ip = nil
	p.at = time.Time{}
}

// lookup 返回当前门户 IP 与解析失败原因（TTL 内命中缓存）。失败一律返回
// 非 nil error：调用方据 net_mode 决定转系统栈还是拒绝——原因要能进路径日志，
// 否则用户只看到「门户解析失败」，查不动为什么。
func (p *portalTarget) lookup() (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ip != nil && p.now().Sub(p.at) < p.ttl {
		return p.ip, nil
	}
	ip, err := p.resolve()
	if err != nil {
		return nil, err
	}
	if ip == nil {
		return nil, errors.New("DNS 解析结果为空")
	}
	p.ip, p.at = ip, p.now()
	return p.ip, nil
}
