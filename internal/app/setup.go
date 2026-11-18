package app

import (
	"errors"
	"fmt"

	"cnal/internal/api"
	"cnal/internal/keeper"
	"cnal/internal/protocol"
)

// SetupError 首跑可修复的启动失败（缺配置/缺 server/login_path/缺凭据）：
// cnalw 捕获后进入首个配置面板，而不是让新机托盘直接报错退出。
// 其余启动失败（TUN 占满、ini 数值非法）仍按错误退出——面板修不了它们。
type SetupError struct {
	Reason string
	Err    error
}

func (e *SetupError) Error() string { return e.Reason }
func (e *SetupError) Unwrap() error { return e.Err }

// AsSetupError 判启动失败是否属于首跑可修复类。
func AsSetupError(err error) (*SetupError, bool) {
	var se *SetupError
	ok := errors.As(err, &se)
	return se, ok
}

// Setup 首跑配置面板的组合根：只接 /state + /config，不触网、不启动守护。
// 用户保存账号密码并关闭窗口后，调用方重新 Compose 走正常启动路径。
type Setup struct {
	API      *api.Server
	Detected string // 自动探测到的门户基址（"" = 未探测到，用户手填）
	cfgPath  string
}

// NewSetup 初始化首跑配置：确保 ini 存在 → （server 缺失时）自动探测门户并
// 补默认端点 → 装配仅配置用的回路 API。探测失败不算错误：面板允许手填。
func NewSetup(cfgPath string) (*Setup, error) {
	if err := ensureIniFile(cfgPath); err != nil {
		return nil, err
	}
	cfgPathCached = cfgPath
	cfg, err := loadDaemonConfig(cfgPath)
	if err != nil && !errors.Is(err, ErrMissingServer) && !errors.Is(err, ErrMissingLoginPath) {
		return nil, err
	}
	s := &Setup{cfgPath: cfgPath}
	if cfg.LoginPath == "" {
		// 既有文件缺端点路径：补 ac_portal 默认（异校仍需核对 README）。
		if err := updateIniFields(map[string]string{"login_path": protocol.DefaultLoginPath}); err != nil {
			return nil, err
		}
	}
	if cfg.Server != "" {
		return s.withAPI(), nil
	}
	detected, derr := DetectPortal(DetectOptions{
		CampusDNS: cfg.CampusDNS, BindIP: cfg.BindIP,
		ProbeIPs: cfg.ProbeIPs, ProbePath: cfg.ProbePath,
		ProbeHost: cfg.ProbeHost, LoginPage: cfg.LoginPage,
		LoginPageMarks: cfg.LoginPageMarks, // 异校登录页指纹（空 = 默认）
	})
	if derr == nil {
		s.Detected = detected
		vals := map[string]string{"server": detected, "login_path": protocol.DefaultLoginPath}
		if err := updateIniFields(vals); err != nil {
			return nil, err
		}
	}
	return s.withAPI(), nil
}

// withAPI 装配首跑面板的回路 API：状态卡显示「首次配置」，配置表单可读写。
func (s *Setup) withAPI() *Setup {
	s.API = api.New()
	s.API.Snapshot = func() api.Snapshot {
		return api.Snapshot{
			State:  "starting",
			Detail: "首次配置：填写账号与密码后保存，关闭窗口即启动守护",
			Label:  "首次配置",
			Color:  api.Color("starting"),
		}
	}
	s.API.GetConfig = func() map[string]string {
		// 每次读文件：用户在设置面板改了 server 再保存，回显要是新值
		//（不能拿启动时快照骗自己）。
		cur, err := loadDaemonConfig(s.cfgPath)
		if err != nil {
			return map[string]string{"username": iniUsername(), "auto_reconnect": "true"}
		}
		return map[string]string{
			"username":       iniUsername(),
			"server":         cur.Server,
			"free_windows":   cur.FreeWindows,
			"campus_dns":     cur.CampusDNS,
			"auto_reconnect": fmt.Sprint(cur.AutoReconnect),
			"bind_ip":        cur.BindIP,
		}
	}
	s.API.ValidateConfig = func(values map[string]string) error {
		if v, ok := values["free_windows"]; ok {
			return keeper.ValidateFreeWindows(v)
		}
		return nil
	}
	s.API.SaveCredentials = func(user, pass string) error {
		if err := credentialSource().Save(user, pass); err != nil {
			return err
		}
		setIniUsername(user) // 凭据已就位：Ready/Compose 的账号指向源同步
		return nil
	}
	s.API.SaveConfig = func(values map[string]string) error {
		// 与运行期面板共用同一条分派（校验先于写入、常用/高级键分组）；
		// password 已被 api 层剥离，绝不落盘。
		return saveConfigValues(values)
	}
	return s
}

// Ready 用户是否已完成最小配置（server 与凭据都可读）。窗口关闭后调用方
// 据此决定重新 Compose 还是提示未完成。
func (s *Setup) Ready() bool {
	cfg, err := loadDaemonConfig(s.cfgPath)
	if err != nil || cfg.Server == "" {
		return false
	}
	_, _, err = loadCredentials()
	return err == nil
}
