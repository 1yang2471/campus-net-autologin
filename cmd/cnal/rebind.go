package main

// PlanRebind 给出 rebind 的有序动作序列。缺失/失败都不发登录：
// S1 不可观测不得变成一次补绑（撞门户反暴力冻结）。
// ErrPeerAbsent 由测试方从 protocol 包 import（计划 Self-Review 第 3 条），
// 本文件只按"err 非空即不发登录"工作，不需要引用具体错误值。
func PlanRebind(dualStack bool, peer string, err error) []string {
	if !dualStack {
		return []string{"纯 v4 登录"}
	}
	if err != nil || peer == "" {
		return nil
	}
	return []string{"查对端", "带 ipv4or6 登录", "复查对端"}
}
