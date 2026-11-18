package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"cnal/internal/app"
)

// exitDetect 未探测到门户（已认证/不在校园网/门户不返回劫持页）——与配置
// 错误分开，脚本可据此提示用户先退出登录再试。
const exitDetect = 6

// runDetectCmd 首跑门户探测（Python detect_portal / compat_check 向导路径同语义）：
// 未认证时公网请求被门户 302 劫持，Location 自报门户地址；再 GET 登录页验
// ac_portal/pwdLogin 指纹。全程无凭据 GET。
func runDetectCmd(args []string) int {
	fs := flag.NewFlagSet("detect", flag.ExitOnError)
	cfgPath := fs.String("config", "", "既存 ini（读取 campus_dns/probe_*；缺省用守护默认路径）")
	write := fs.Bool("write", false, "探测成功后写最小配置（仅当文件不存在）")
	campusDNS := fs.String("campus-dns", "", "校园 DNS（覆盖 ini；门户域名解析用）")
	bindIP := fs.String("bind-ip", "", "物理源 v4 逃生口（空 = 自动发现）")
	timeout := fs.Int("timeout", 4, "单请求超时秒数")
	fs.Parse(args)

	path := *cfgPath
	if path == "" {
		path = app.ConfigFilePath()
	}
	opt := app.DetectOptions{
		CampusDNS: *campusDNS,
		BindIP:    *bindIP,
		Timeout:   time.Duration(*timeout) * time.Second,
	}
	// 已有一份可用配置（含 Python 生成）时，复用其校园 DNS/锚点——异校的
	// 锚点与 DNS 是校事实，探测器不该拿通用默认去猜。
	if cfg, err := loadConfig(path); err == nil {
		if opt.CampusDNS == "" {
			opt.CampusDNS = cfg.CampusDNS
		}
		if opt.BindIP == "" {
			opt.BindIP = cfg.BindIP
		}
		opt.ProbeIPs = cfg.ProbeIPs
		opt.ProbePath = cfg.ProbePath
		opt.ProbeHost = cfg.ProbeHost
		opt.LoginPage = cfg.LoginPage
		opt.LoginPageMarks = cfg.LoginPageMarks // 异校登录页指纹（空 = 默认）
	}

	base, err := app.DetectPortal(opt)
	if err != nil {
		fmt.Println("探测失败:", err)
		fmt.Println("提示: 已登录时先退出登录/断开重连后重试；或手工在 ini 填 server（README 异校适配）。")
		return exitDetect
	}
	fmt.Println("门户:", base)

	if !*write {
		fmt.Printf("下一步: cnal detect --write 生成最小配置，或在 ini 写 server = %s\n", base)
		return exitOK
	}
	if err := app.WriteStarterConfig(path, base); err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Println("配置已存在，未改动:", path)
			fmt.Println("如需更新 server，请手工编辑或使用面板设置。")
			return exitOK
		}
		fmt.Println("写配置失败:", err)
		return exitCfg
	}
	fmt.Println("已写入最小配置:", path)
	fmt.Println("下一步: 启动 cnalw（首次会打开面板填写账号密码；密码只进 Windows 凭据管理器）。")
	return exitOK
}
