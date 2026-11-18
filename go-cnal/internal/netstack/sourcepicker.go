package netstack

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
)

// SourcePicker 按**门户可达性**挑选物理源（2026-11-11 设计 §3）。
//
// 候选按序探测，第一个通过的即选中；全部失败则保留原选中源（fail-closed：
// 不用坏源覆盖好源）并返回带各候选原因的错误。
// 探测函数由调用方注入——本包只管「按序试、记原因」，不关心门户长什么样。
type SourcePicker struct {
	mu         sync.Mutex
	candidates func() []net.IP
	probe      func(net.IP) error
	cur        net.IP
	fails      map[string]error
	order      []net.IP // 最近一次探测的候选顺序（摘要按它排）
}

// NewSourcePicker 造选源器。candidates 每次调用重新枚举（网卡会变），
// probe 判定某源能不能到门户。
func NewSourcePicker(candidates func() []net.IP, probe func(net.IP) error) *SourcePicker {
	return &SourcePicker{
		candidates: candidates,
		probe:      probe,
		fails:      map[string]error{},
	}
}

// Current 当前选中的源（还没探测过则 nil）。
func (p *SourcePicker) Current() net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cur
}

// Refresh 重新探测选源。返回选中的源；一个都不通时返回 nil + 错误（错误里逐个
// 候选列出原因，调用方直接写日志/面板）。
func (p *SourcePicker) Refresh() (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.candidates == nil || p.probe == nil {
		return p.cur, nil
	}
	cands := p.candidates()
	if len(cands) == 0 {
		return p.cur, errors.New("没有可用物理 v4 候选源")
	}
	fails := map[string]error{}
	for _, c := range cands {
		err := p.probe(c)
		if err == nil {
			p.cur = c
			p.fails = fails
			p.order = cands
			return c, nil
		}
		fails[c.String()] = err
	}
	p.fails = fails
	p.order = cands
	return p.cur, fmt.Errorf("候选源 %d 个均不可达门户: %s",
		len(cands), formatFails(cands, fails))
}

// Invalidate 作废当前选中源（该源被门户操作判失败）——下次 Refresh 重新探测。
func (p *SourcePicker) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cur = nil
}

// Failures 最近一次探测各候选的失败原因（面板/日志用）。
func (p *SourcePicker) Failures() map[string]error {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]error, len(p.fails))
	for k, v := range p.fails {
		out[k] = v
	}
	return out
}

// Summary 最近一次探测的一句话总结（按候选顺序）：
// "10.141.53.34 不通（i/o timeout）；172.29.153.208 通"。
// 没探测过则返回空串。
func (p *SourcePicker) Summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p.order))
	for _, c := range p.order {
		if err := p.fails[c.String()]; err != nil {
			parts = append(parts, fmt.Sprintf("%s 不通（%s）", c, err))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s 通", c))
	}
	return strings.Join(parts, "；")
}

// formatFails 把各候选的失败原因拼成一行（顺序跟候选顺序一致，便于对照）。
func formatFails(cands []net.IP, fails map[string]error) string {
	parts := make([]string, 0, len(cands))
	for _, c := range cands {
		err := fails[c.String()]
		if err == nil {
			err = errors.New("未知原因")
		}
		parts = append(parts, fmt.Sprintf("%s（%s）", c, err))
	}
	return strings.Join(parts, "；")
}
