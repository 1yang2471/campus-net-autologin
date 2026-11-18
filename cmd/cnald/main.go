package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"cnal/internal/app"
)

// cnald 无头守护：组合根在 internal/app（与 cnalw 托盘共吃一份接线）。
func main() {
	fs := flag.NewFlagSet("cnald", flag.ExitOnError)
	cfgPath := fs.String("config", "", "守护同款 ini 路径（[keeper] 节）")
	fs.Parse(os.Args[1:])
	if *cfgPath == "" {
		fs.Usage()
		os.Exit(2)
	}
	os.Exit(run(*cfgPath))
}

func run(cfgPath string) int {
	// 与 cnalw 共用同一把单实例锁（两个守护并行会互抢登录撞门户限频）。
	if release, ok := app.AcquireInstanceLock(); !ok {
		fmt.Fprintln(os.Stderr, "已有守护实例在运行，本实例退出（不重复登录）")
		return 0
	} else {
		defer release()
	}
	a, err := app.Compose(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	addr, err := a.API.Listen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "回路 API 启动失败:", err)
		return 2
	}
	// 与 cnalw 同一份端点文件：`cnal status` 不区分无头/托盘壳。
	if cleanup, err := app.WriteRuntimeFile(app.RuntimeFilePath(), "http://"+addr.String(), a.API.Token); err != nil {
		fmt.Fprintln(os.Stderr, "端点文件写入失败（cnal status 将不可用）:", err)
	} else {
		defer cleanup()
	}
	fmt.Printf("cnald 启动：interval=%s free_windows=%q\n", a.Cfg.Interval, a.Cfg.FreeWindows)
	last := ""
	for {
		a.Daemon.Tick()
		state, detail, _, _, _ := a.Daemon.View()
		line := state + "|" + detail
		if line != last {
			fmt.Printf("%s [%s] %s\n", time.Now().Format("15:04:05"), state, detail)
			last = line
		}
		time.Sleep(a.Cfg.Interval)
	}
}
