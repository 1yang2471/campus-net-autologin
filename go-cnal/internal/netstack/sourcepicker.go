package netstack

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
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
	order      []net.IP        // 最近一次探测的候选顺序（摘要按它排）
	tried      map[string]bool // 最近一次真正探测过的候选（摘要不得把未探测写成「通」）
	downUntil  map[string]time.Time // 刚选中就失效的候选降级窗口
	now        func() time.Time
}

// NewSourcePicker 造选源器。candidates 每次调用重新枚举（网卡会变），
// probe 判定某源能不能到门户。
func NewSourcePicker(candidates func() []net.IP, probe func(net.IP) error) *SourcePicker {
	return &SourcePicker{
		candidates: candidates,
		probe:      probe,
		fails:      map[string]error{},
		tried:      map[string]bool{},
		downUntil:  map[string]time.Time{},
		now:        time.Now,
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
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	// 过期降级清理：候选回来时重新参与，不让 map 无限长。
	for k, until := range p.downUntil {
		if !now.Before(until) {
			delete(p.downUntil, k)
		}
	}
	fails := map[string]error{}
	tried := map[string]bool{}
	active := make([]net.IP, 0, len(cands))
	for _, c := range cands {
		if until, ok := p.downUntil[c.String()]; ok && now.Before(until) {
			fails[c.String()] = fmt.Errorf("降级中（%s 后重试）", until.Sub(now).Round(time.Second))
			continue
		}
		active = append(active, c)
	}
	p.order = cands
	p.tried = tried
	if len(active) == 0 {
		p.fails = fails
		return p.cur, fmt.Errorf("候选源 %d 个均处于降级期: %s",
			len(cands), formatFails(cands, fails))
	}
	for _, c := range active {
		tried[c.String()] = true
		err := p.probe(c)
		if err == nil {
			p.cur = c
			p.fails = fails
			return c, nil
		}
		fails[c.String()] = err
	}
	p.fails = fails
	return p.cur, fmt.Errorf("候选源 %d 个均不可达门户: %s",
		len(cands), formatFails(cands, fails))
}

// candidateDownFor 刚选中就失效的候选降级时长（2026-11-18 实证：17:47:39
// 选中 10.141.53.34，40 秒后绑源 DNS 超时；17:56:13 选中热点 10.158.52.162，
// 27 秒后 bind 失效）。60 秒跨过「用户正在切网」的抖动窗口，又不会把真正
// 恢复的网卡锁太久。
const candidateDownFor = 60 * time.Second

// Down 标记候选源降级（刚选中就失效/绑定失败）：Refresh 在窗口内跳过它，
// 避免反复选回刚死的候选。只影响「下一次选谁」，不改当前绑定——当前绑定
// 由组合根的 apply 负责。
func (p *SourcePicker) Down(ip net.IP, now time.Time) {
	if ip == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.downUntil[ip.String()] = now.Add(candidateDownFor)
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
// 没探测过则返回空串。前序候选已通时后续候选没有被探测，必须写「未探测」——
// 写成「通」是诊断假话（2026-11-18 日志：只有首个候选探过，摘要却把两个都
// 写成「通」）。
func (p *SourcePicker) Summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p.order))
	for _, c := range p.order {
		key := c.String()
		if err := p.fails[key]; err != nil {
			parts = append(parts, fmt.Sprintf("%s 不通（%s）", c, err))
			continue
		}
		if !p.tried[key] {
			parts = append(parts, fmt.Sprintf("%s 未探测", c))
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
