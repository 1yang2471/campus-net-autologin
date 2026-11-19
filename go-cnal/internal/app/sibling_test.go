package app

import "testing"

// 旁路网卡认证态判定（2026-11-19 实测形态）：
// 未认证 VLAN 下白名单外 HTTP 被 302 到门户（Location 自报门户地址）；
// 认证后是正常响应。连不上/没发出去 = 未知，绝不能误报「未认证」。
func TestSiblingVerdict(t *testing.T) {
	const portal = "a.stu.edu.cn"
	marks := []string{"上网认证系统"}
	cases := []struct {
		name     string
		status   int
		location string
		body     string
		want     string
	}{
		{"302 指门户 = 未认证", 302,
			"https://a.stu.edu.cn:443/ac_portal/proxy.html?url=http://www.baidu.com/",
			"", "unauth"},
		{"302 到自己 https = 已认证（正常跳转）", 302,
			"https://www.baidu.com/", "", "authed"},
		{"200 + 门户指纹 = 未认证", 200, "", "<title>上网认证系统</title>", "unauth"},
		{"200 普通内容 = 已认证", 200, "", "<html>baidu</html>", "authed"},
		{"204 = 已认证", 204, "", "", "authed"},
		{"连不上 = 未知", 0, "", "", "unknown"},
		{"403 = 未知（说明不了认证态）", 403, "", "", "unknown"},
	}
	for _, c := range cases {
		if got := siblingVerdict(c.status, c.location, c.body, portal, marks); got != c.want {
			t.Errorf("%s: 期望 %s，实际 %s", c.name, c.want, got)
		}
	}
}
