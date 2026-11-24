package app

import (
	"net"
	"sync"
)

// 安卓注入缝：四个平台差异点（凭据/路径/物理源/打印）集中到这一处，
// 由 mobile 桥在 Compose 之前调用。Windows 侧零调用 => 行为不变。
// 计划见 go-cnal/docs/android-plan.md §3。

// CredentialSource 凭据读写缝。Windows 由凭据管理器实现（store 包，
// 见 inject_windows.go）；安卓由 Kotlin Keystore 解密后注入明文，只在内存。
// Load 按用户名取，LoadLegacy 是旧单条凭据（simple target）的回落读取——
// Windows 有、安卓没有，明确报错而不是静默返回空。
type CredentialSource interface {
	Load(user string) (string, error)
	LoadLegacy() (user, pass string, err error)
	Save(user, pass string) error
}

var (
	credMu  sync.RWMutex
	credSrc CredentialSource
)

// SetCredentialSource 覆盖凭据来源；nil = 恢复平台默认（Windows 凭据管理器）。
func SetCredentialSource(c CredentialSource) {
	credMu.Lock()
	defer credMu.Unlock()
	credSrc = c
}

// credentialSource 当前生效的凭据来源（未注入时回平台默认）。
func credentialSource() CredentialSource {
	credMu.RLock()
	defer credMu.RUnlock()
	if credSrc != nil {
		return credSrc
	}
	return defaultCredentialSource()
}

// dataDirOverride 安卓传入的 context.filesDir；空 = Windows 默认（LOCALAPPDATA）。
var (
	dataDirMu       sync.RWMutex
	dataDirOverride string
)

// SetDataDir 覆盖守护数据目录（config/log/lock/api.json 同一处）。
// 空串 = 恢复默认。Compose 之前调用。
func SetDataDir(dir string) {
	dataDirMu.Lock()
	defer dataDirMu.Unlock()
	dataDirOverride = dir
}

// dataDir 守护数据目录：安卓注入优先，否则平台默认。
func dataDir() string {
	dataDirMu.RLock()
	d := dataDirOverride
	dataDirMu.RUnlock()
	if d != "" {
		return d
	}
	return defaultDataDir()
}

// PhysicalSources 物理源注入：Kotlin 从 LinkProperties 取 wlan0 的 v4 与
// 全局 GUA v6 传进来。空 = 不覆盖，Go 自己发现（Windows 路径）。
type PhysicalSources struct {
	V4 net.IP
	V6 net.IP
}

var (
	srcMu        sync.RWMutex
	srcInjected  bool
	srcV4, srcV6 net.IP
)

// SetPhysicalSources 注入物理源；V6 可为 nil（v1 手机只 v4）。
// 传空 PhysicalSources{} = 取消注入，回到自动发现。
func SetPhysicalSources(s PhysicalSources) {
	srcMu.Lock()
	defer srcMu.Unlock()
	srcInjected = s.V4 != nil || s.V6 != nil
	srcV4, srcV6 = s.V4, s.V6
}

// physicalSources 当前注入的物理源；injected=false 表示未覆盖。
func physicalSources() (v4, v6 net.IP, injected bool) {
	srcMu.RLock()
	defer srcMu.RUnlock()
	return srcV4, srcV6, srcInjected
}

// CurrentPhysicalSources 注入值的只读快照（安卓 A4 绑网核对要用：拿注进来的
// 源地址做一次绑源请求，看门户认到的出口 IP 是不是它）。未注入时返回零值。
func CurrentPhysicalSources() PhysicalSources {
	srcMu.RLock()
	defer srcMu.RUnlock()
	if !srcInjected {
		return PhysicalSources{}
	}
	return PhysicalSources{V4: srcV4, V6: srcV6}
}

// 注入模式的两个包级缝（仅安卓用，Windows 零调用）：
//   - activeRebind：已 Compose 的 plumbing 的重绑入口（换网后 Mobile.RebindSources 调）；
//   - sourceStaleSink：Kotlin 实现的 SourceStale 消费者（绑定失效时通知重新注入）。
//
// 都在 newPlumbing 注入模式注册；非注入模式为 nil = no-op。
var (
	injectMu        sync.RWMutex
	activeRebind    func() error
	sourceStaleSink func()
)

// RegisterInjectedPlumbing 由 newPlumbing 在注入模式注册重绑入口。
func RegisterInjectedPlumbing(rebind func() error) {
	injectMu.Lock()
	activeRebind = rebind
	injectMu.Unlock()
}

// RebindInjectedSources 安卓换网后重绑注入源（AAR 导出 Mobile.RebindSources 的
// Go 实现）。未注册（非注入/未 Compose）时 no-op。
func RebindInjectedSources() error {
	injectMu.RLock()
	fn := activeRebind
	injectMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// SetSourceStaleSink 注入 SourceStale 消费者（D-09，Kotlin 实现）。
func SetSourceStaleSink(sink func()) {
	injectMu.Lock()
	sourceStaleSink = sink
	injectMu.Unlock()
}

// NotifySourceStale 通知 Kotlin 重新注入物理源（注入模式绑定失效时由 plumbing
// 调用；导出亦供 JNI 桥接/测试直接触发）。无消费者时 no-op。
func NotifySourceStale() {
	injectMu.RLock()
	sink := sourceStaleSink
	injectMu.RUnlock()
	if sink != nil {
		sink()
	}
}

// SourcePrinter 网络层诊断打印缝（物理源/重发现/门户选路）：Windows 打到
// stdout，安卓 stdout 不可见，注入写状态日志的实现。nil = 不打印。
type SourcePrinter func(format string, args ...any)

var (
	printerMu  sync.RWMutex
	srcPrinter SourcePrinter = printSourceStdout
)

// SetSourcePrinter 覆盖物理源打印；nil = 静默。
func SetSourcePrinter(p SourcePrinter) {
	printerMu.Lock()
	defer printerMu.Unlock()
	srcPrinter = p
}

func printSource(format string, args ...any) {
	printerMu.RLock()
	p := srcPrinter
	printerMu.RUnlock()
	if p != nil {
		p(format, args...)
	}
}
