package keeper

import (
	"regexp"
	"strconv"
	"strings"
)

// 流量能力升级：把 userflux 的「带单位字符串」归一成字节数，供面板算
// 已用/额度/百分比。字段名是校事实（不做键名假设），这里只管量纲写法。
//
// 真机语料：2.40006G / 603.281M / 4G / 1.2 GB / 512K。认不出的写法一律
// 返回 false，绝不猜个数——否则「已用 80%」可能是错的，用户会据此误判。
var reFluxSize = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*([kmgt]?)(?:b|byte|bytes)?\s*$`)

var fluxUnits = map[string]float64{
	"k": 1024,
	"m": 1024 * 1024,
	"g": 1024 * 1024 * 1024,
	"t": 1024 * 1024 * 1024 * 1024,
}

// ParseFluxBytes 解析流量字符串为字节数。ok=false 表示算不出（格式不认识），
// 调用方应把原文照列、百分比留空，而不是显示一个编造的数字。
func ParseFluxBytes(s string) (float64, bool) {
	m := reFluxSize.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	unit, ok := fluxUnits[strings.ToLower(m[2])]
	if !ok {
		return 0, false
	}
	return v * unit, true
}

// FluxUsageView 面板/通知/回路 API 共用的只读用量视图。Raw 是脱敏后的原始
// 键值对照（用户名称不带出——凭据纪律），解析失败时它仍在，百分比留空。
type FluxUsageView struct {
	Used       string            `json:"used,omitempty"`
	Quota      string            `json:"quota,omitempty"`
	Percent    float64           `json:"percent,omitempty"`
	HasPercent bool              `json:"has_percent"`
	Level      string            `json:"level,omitempty"` // ok / warn / critical
	Raw        map[string]string `json:"raw,omitempty"`
}

// 键名优先级：参考校「当天流量/日流量额」优先，异校常见别名兜底。
var (
	fluxUsedKeys  = []string{"当天流量", "已用流量", "使用流量", "已用"}
	fluxQuotaKeys = []string{"日流量额", "流量额度", "总额度", "套餐流量"}
)

func firstFluxValue(data map[string]string, keys []string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(data[k]); v != "" {
			return v
		}
	}
	return ""
}

// FluxUsage 从 userflux 键值生成用量视图。字段名是校事实，所以只按已知别名取；
// 取不到/算不出 → HasPercent=false，原文照列，绝不猜数（异校零回归）。
func FluxUsage(data map[string]string) FluxUsageView {
	v := FluxUsageView{Raw: map[string]string{}, Level: "ok"}
	for k, val := range data {
		if k != "用户名称" && strings.TrimSpace(val) != "" {
			v.Raw[k] = val
		}
	}
	v.Used = firstFluxValue(data, fluxUsedKeys)
	v.Quota = firstFluxValue(data, fluxQuotaKeys)
	if v.Used == "" || v.Quota == "" {
		return v
	}
	used, okU := ParseFluxBytes(v.Used)
	quota, okQ := ParseFluxBytes(v.Quota)
	if !okU || !okQ || quota <= 0 {
		return v
	}
	v.HasPercent = true
	v.Percent = used / quota * 100
	if v.Percent > 100 {
		v.Percent = 100
	}
	if v.Percent < 0 {
		v.Percent = 0
	}
	return v
}
