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

	// 行为确认（指纹快筛失败后的第二路径）。首跑是「先探测、后填凭据」——
	// 凭据只在面板保存时经 /config 拿到，故确认挂在 SaveConfig 缝上。
	// NeedConfirm = DetectPortal 指纹未命中（或 server 本就由用户手填未探测）。
	NeedConfirm bool
	// ConfirmEvidence 行为确认的证据文案（命中/判负/发包失败），供状态卡 detail
	// 回显给用户；空 = 未触发或无结论。
	ConfirmEvidence string

	// pendingUser/pendingPass 本次面板保存提交里携带的凭据（成对时非空）。
	// 只在内存暂存于本次请求，用于行为确认填表，绝不写 ini。
	pendingUser, pendingPass string
	// confirmFunc 行为确认缝：生产接 confirmSetupWithPlumbing（走守护投递路径）；
	// 测试注入替身，不发真实凭据、不触网。
	confirmFunc func(daemonConfig, string, string) (bool, string, error)
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
		// 既有 server（用户手填/上次残留）没经过本次指纹快筛——标记待确认，
		// 凭据填好后由 SaveConfig 缝跑一次行为确认。
		s.NeedConfirm = true
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
	} else {
		// 指纹快筛失败（异校页指纹不匹配 / 已登录 / 不在校园网）：标记待确认，
		// 等用户在面板填好凭据后走行为确认第二路径。
		s.NeedConfirm = true
	}
	return s.withAPI(), nil
}

// withAPI 装配首跑面板的回路 API：状态卡显示「首次配置」，配置表单可读写。
// 指纹快筛失败后，凭据在面板保存时到手，SaveConfig 缝跑一次行为确认（第二
// 路径），结论（含失败证据）经状态卡 detail 回显给用户。
func (s *Setup) withAPI() *Setup {
	s.API = api.New()
	s.API.Snapshot = func() api.Snapshot {
		detail := "首次配置：填写账号与密码后保存，关闭窗口即启动守护"
		if s.ConfirmEvidence != "" {
			detail = s.ConfirmEvidence // 行为确认证据（成功/判负/发包失败）如实回显
		}
		return api.Snapshot{
			State:  "starting",
			Detail: detail,
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
		// 供本次 SaveConfig 缝做行为确认填表（只在内存暂存，不写 ini、不另存）。
		s.pendingUser, s.pendingPass = user, pass
		return nil
	}
	s.API.SaveConfig = func(values map[string]string) error {
		// 与运行期面板共用同一条分派（校验先于写入、常用/高级键分组）；
		// password 已被 api 层剥离，绝不落盘。
		if err := saveConfigValues(values); err != nil {
			return err
		}
		// 指纹快筛失败后的第二路径：凭据已就位，按当前 cfg 发一次探测登录，
		// 用守护同款判定链判定。成功 → 与指纹成功路径一样继续；失败 → 给证据。
		// 判负/发包失败都不撤销已保存配置——保持面板「手填后保存」原有失败处理。
		if s.NeedConfirm {
			cfg, err := loadDaemonConfig(s.cfgPath)
			if err != nil {
				s.ConfirmEvidence = "行为确认读配置失败：" + err.Error()
				return nil // 配置已落盘，不阻断手填路径
			}
			user, pass := s.pendingUser, s.pendingPass
			if user == "" || pass == "" {
				// 未在本次提交里填密码（留空=不修改凭据）：从已存凭据读取。
				user, pass, err = loadCredentials()
				if err != nil {
					s.ConfirmEvidence = "行为确认缺凭据：" + err.Error()
					return nil
				}
			}
			confirm := s.confirmFunc
			if confirm == nil {
				confirm = confirmSetupWithPlumbing
			}
			proceed, evidence := SetupConfirmDecision(false, func() (bool, string, error) {
				return confirm(cfg, user, pass)
			})
			s.ConfirmEvidence = evidence
			if proceed {
				s.NeedConfirm = false // 已确认可用，后续保存不再重复探测
			}
		}
		return nil
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
