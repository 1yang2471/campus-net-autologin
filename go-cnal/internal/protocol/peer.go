package protocol

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"cnal/internal/netstack"
)

// Peer 是"对端栈入口"：v4 页面问 v6 侧、v6 页面问 v4 侧（门户前端如此，E1）。
// 它只承载无凭据握手——构造上就没有凭据参数，而不是"记得不传"。
type Peer struct {
	mu       sync.Mutex
	fullURL  string // 完整 URL（含 scheme 与端点路径），构造时统一，Query 不再拼接
	timeout  time.Duration
	bindMode netstack.BindMode

	validatedCount int
	lastValidated  time.Time
}

const defaultPeerTimeout = 5 * time.Second

// NewPeer 接受完整 URL，构造时不校验——每次 Query 前都会重新校验。
func NewPeer(fullURL string, timeout time.Duration) *Peer {
	return NewPeerWithBindMode(fullURL, timeout, netstack.DefaultBindMode())
}

func NewPeerWithBindMode(fullURL string, timeout time.Duration, mode netstack.BindMode) *Peer {
	return &Peer{fullURL: fullURL, timeout: timeout, bindMode: mode}
}

func (p *Peer) LastValidated() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastValidated
}

func (p *Peer) ValidatedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.validatedCount
}

// ValidatePeer：只接受 IP 字面量 + 默认端口 + http/https（规格 §6.1 第 4 条）。
// 环回/链路本地/组播/未指定无条件拒绝——SSRF 经典落点（含 169.254.169.254 云
// 元数据），校园场景没有任何正当理由访问它们。私有/保留段放行：门户本就住在
// 对端网内，真正的风险面是凭据流向，而凭据永远只发往用户确认过的 server，
// 握手在构造上无凭据（Task 4 补 SSRF 分级表）。URL 里不许有 query/userinfo/
// fragment——那都是把凭据偷渡进请求行的口子。
func ValidatePeer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("只允许 http/https: %s", u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("对端 URL 必须是裸端点，不许带 query/userinfo/fragment")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("对端必须是 IP 字面量，不接受域名")
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() {
		return fmt.Errorf("禁止环回/链路本地/组播/未指定地址对端: %s", host)
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return fmt.Errorf("对端端口非默认，拒绝: %s", p)
	}
	return nil
}

// HandshakeForm 握手请求体的唯一构造点。参数只有操作名——操作名是词表
// （校事实，2026-11-07 实测真名 ipv4or6online），任何想加凭据的改法都会
// 让这个函数显得刺眼（规格 §7 字节级无凭据）。
func HandshakeForm(opr string) map[string]string {
	return map[string]string{"opr": opr}
}

// Query 发无凭据握手，返回原始响应体。每次调用先重新校验
// （提取值可能随门户更新变化，一次性校验+长期复用是给污染值开门）。
func (p *Peer) Query(src net.IP, opr string) (string, error) {
	return p.PostForm(src, HandshakeForm(opr))
}

// PostForm 发任意无凭据表单（按栈 op 用：op 词表 + 该栈地址）。
// Query 是它的特例——同一套 URL 校验、同一套超时，不造第二条请求路径。
func (p *Peer) PostForm(src net.IP, form map[string]string) (string, error) {
	if err := ValidatePeer(p.fullURL); err != nil {
		return "", err
	}
	p.mu.Lock()
	p.validatedCount++
	p.lastValidated = time.Now()
	p.mu.Unlock()

	c := netstack.NewWithBindMode(src, p.timeout, p.bindMode)
	return c.Post(p.fullURL, form, nil)
}
