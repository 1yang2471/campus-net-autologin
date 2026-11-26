package app

import (
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
)

// 注入缝的验收点：安卓注入的值必须真的到达组合根的各处消费点，不是只存进
// 一个全局变量没人读。每个测试都把功能故意弄坏就会红（见各用例注释）。

type fakeCreds struct {
	mu                     sync.Mutex
	saved                  map[string]string
	load                   string
	legacyUser, legacyPass string
}

func (f *fakeCreds) Load(user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.saved[user]; ok {
		return p, nil
	}
	return f.load, nil
}

func (f *fakeCreds) LoadLegacy() (string, string, error) {
	return f.legacyUser, f.legacyPass, nil
}

func (f *fakeCreds) Save(user, pass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saved == nil {
		f.saved = map[string]string{}
	}
	f.saved[user] = pass
	return nil
}

func TestSetCredentialSourceDrivesLoadAndSave(t *testing.T) {
	f := &fakeCreds{saved: map[string]string{"alice": "pw-a"}}
	SetCredentialSource(f)
	t.Cleanup(func() { SetCredentialSource(nil) })

	setIniUsername("alice")
	t.Cleanup(func() { setIniUsername("") })
	u, p, err := loadCredentials()
	if err != nil || u != "alice" || p != "pw-a" {
		t.Fatalf("注入的凭据未生效：u=%q p=%q err=%v", u, p, err)
	}
	// Save 走的是同一个注入源：面板保存账号后下一次 Load 要读得到。
	if err := credentialSource().Save("bob", "pw-b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Load("bob"); got != "pw-b" {
		t.Fatalf("Save 未落到注入源：%q", got)
	}
}

func TestUninjectedCredentialsFailLoudly(t *testing.T) {
	// Windows 默认源可用，这个用例只在非 Windows 平台有意义；用注入一个
	// 显式报错的源来守「绝不静默返回空密码」这条通用契约。
	SetCredentialSource(failingCreds{})
	t.Cleanup(func() { SetCredentialSource(nil) })
	setIniUsername("alice")
	t.Cleanup(func() { setIniUsername("") })
	if _, _, err := loadCredentials(); err == nil {
		t.Fatal("注入源报错时不应静默当作无凭据继续")
	}
}

type failingCreds struct{}

func (failingCreds) Load(string) (string, error) {
	return "", errors.New("no cred")
}
func (failingCreds) LoadLegacy() (string, string, error) { return "", "", errors.New("no cred") }
func (failingCreds) Save(string, string) error           { return errors.New("no cred") }

func TestSetDataDirDrivesAllPaths(t *testing.T) {
	dir := t.TempDir()
	SetDataDir(dir)
	t.Cleanup(func() { SetDataDir("") })

	// 四个路径函数必须都落在注入目录下，否则安卓的配置/日志/端点文件
	// 会写到 Windows 默认位置（模拟器上看不见，A1/A3 的算术依据就丢了）。
	for name, got := range map[string]string{
		"config.ini": ConfigFilePath(),
		"cnalw.log":  LogFilePath(),
		"api.json":   RuntimeFilePath(),
		"cnalw.lock": InstanceLockPath(),
	} {
		want := filepath.Join(dir, name)
		if got != want {
			t.Errorf("%s：期望 %q，实际 %q", name, want, got)
		}
	}
}

func TestSetPhysicalSourcesDrivesPlumbingAndSnapshot(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	v6 := net.ParseIP("2001:db8::1")
	SetPhysicalSources(PhysicalSources{V4: v4, V6: v6})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })

	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	cfg.BindIP = "" // 不手工钉源，注入值才生效
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if got := pl.currentV4(); !got.Equal(v4) {
		t.Errorf("注入的 v4 未进 plumbing：%v", got)
	}
	if got := pl.currentV6(); !got.Equal(v6) {
		t.Errorf("注入的 v6 未进 plumbing：%v", got)
	}
	if got := v6Source(pl.currentV6); !got.Equal(v6) {
		t.Errorf("handshake/注销读到的不是注入的 v6：%v", got)
	}
	// Snapshot 的 V6Src 必须读注入值，不能重新枚举网卡（安卓上会选到 rmnet）。
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)
	if got := a.Snapshot().V6Src; got != v6.String() {
		t.Errorf("面板 V6Src 显示 %q，期望注入的 %q", got, v6.String())
	}
}

// TestRebindInjectedV4UpdatesBoundClients 是源失效自愈的 tracer bullet：
// 安卓换 WiFi 后 Kotlin 重新注入 v4，守护必须把四个绑源客户端一起改绑，
// 否则 currentV4 快照变了、实际流量仍从旧 IP 出去（4 小时 bind 失效的成因）。
// 断言走公开读数（Snapshot.v4_src + Client.Source()），不碰内部状态。
func TestRebindInjectedV4UpdatesBoundClients(t *testing.T) {
	old := net.ParseIP("10.20.30.40")
	fresh := net.ParseIP("10.20.30.99")
	SetPhysicalSources(PhysicalSources{V4: old})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)

	// 重注入新 v4 + 显式重绑（用户拍板的接口：Mobile.RebindSources → 本函数）。
	SetPhysicalSources(PhysicalSources{V4: fresh})
	if err := pl.rebindInjectedSources(); err != nil {
		t.Fatalf("重绑失败: %v", err)
	}

	if got := pl.currentV4(); !got.Equal(fresh) {
		t.Errorf("重绑后 currentV4=%v，期望 %v", got, fresh)
	}
	for name, c := range map[string]interface{ Source() net.IP }{
		"portal": pl.portal, "probe": pl.probe, "login": pl.login, "tlsProbe": pl.tlsProbe,
	} {
		if got := c.Source(); !got.Equal(fresh) {
			t.Errorf("重绑后 %s 客户端仍绑 %v，期望 %v", name, got, fresh)
		}
	}
	if got := a.Snapshot().V4Src; got != fresh.String() {
		t.Errorf("重绑后面板 V4Src=%q，期望 %q", got, fresh.String())
	}
}

// TestRebindViaExportedEntry 守端到端接线：Kotlin 调 Mobile.RebindSources →
// app.RebindInjectedSources → 已注册 plumbing 的 rebindInjectedSources。
// 直接调 pl.rebindInjectedSources 的测试会漏掉「注册没接上」，这个用例补上。
func TestRebindViaExportedEntry(t *testing.T) {
	old := net.ParseIP("10.20.30.40")
	fresh := net.ParseIP("10.20.30.99")
	SetPhysicalSources(PhysicalSources{V4: old})
	t.Cleanup(func() {
		SetPhysicalSources(PhysicalSources{})
		RegisterInjectedPlumbing(nil) // 清理全局，避免污染后续测试
	})
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p") // 注入模式自动注册 activeRebind
	if err != nil {
		t.Fatal(err)
	}

	// 经导出入口改注入源并重绑（真实 Kotlin 调用链）。
	SetPhysicalSources(PhysicalSources{V4: fresh})
	if err := RebindInjectedSources(); err != nil {
		t.Fatalf("导出重绑失败: %v", err)
	}
	if got := pl.currentV4(); !got.Equal(fresh) {
		t.Fatalf("导出入口未改绑：currentV4=%v，期望 %v", got, fresh)
	}
	if got := pl.portal.Source(); !got.Equal(fresh) {
		t.Fatalf("导出入口未改绑客户端：portal.Source=%v，期望 %v", got, fresh)
	}
}

// TestSourceStaleFiresOnceUntilRebind 守注入模式的自愈信号：绑源失效时
// chooseRoute 必须通知一次 SourceStale，且连续多拍不重复（防 Bindable 抖动风暴），
// 重绑成功后信号复位、下次失效可再触发。这是 D-09 的可观察行为契约。
func TestSourceStaleFiresOnceUntilRebind(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	SetPhysicalSources(PhysicalSources{V4: v4})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	// 门户 IP 解析替身（否则 pt.lookup 无缓存会失败，走不到 bindable 分支）。
	pl.pt.resolveFor = func(net.IP) (net.IP, error) { return net.ParseIP("1.2.3.4"), nil }
	pl.bindable = func(net.IP) bool { return false } // 源不可绑定

	fired := 0
	pl.sourceStale = func() { fired++ }

	// 连续三拍都失效：只应通知一次。
	for i := 0; i < 3; i++ {
		pl.chooseRoute(pl.portal, pl.systemPortal)
	}
	if fired != 1 {
		t.Fatalf("SourceStale 触发 %d 次，期望 1 次（防抖失效）", fired)
	}

	// 重绑成功（**同一 IP**——Android WiFi 重关联后 DHCP 常给同一个地址，
	// 只是网卡绑定丢了）后信号必须复位；再次失效应能重新触发。
	// 若复位绑在「IP 变了」上，此处 fired 会停在 1——曾真实存在此缺陷。
	if err := pl.rebindInjectedSources(); err != nil {
		t.Fatal(err)
	}
	pl.bindable = func(net.IP) bool { return false }
	pl.chooseRoute(pl.portal, pl.systemPortal)
	if fired != 2 {
		t.Fatalf("同 IP 重绑后再次失效，SourceStale 累计 %d 次，期望 2 次（信号未复位）", fired)
	}
}

// TestInjectedPortalDownNotifiesSourceStale 守注入模式的第二个自愈信号：
// 「绑源有效但到不了门户」（连到无校园路由的 Wi-Fi）走 OnPortalDown，
// 注入模式下也必须通知 Kotlin 重注入，不能什么都不做（trouble-maker 重要项）。
func TestInjectedPortalDownNotifiesSourceStale(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	SetPhysicalSources(PhysicalSources{V4: v4})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	d := newDaemon(cfg, pl)
	fired := 0
	pl.sourceStale = func() { fired++ }
	if d.OnPortalDown == nil {
		t.Fatal("注入模式下 OnPortalDown 未装配——门户不可达信号被吞")
	}
	d.OnPortalDown()
	if fired != 1 {
		t.Fatalf("注入模式门户不可达未通知 SourceStale：fired=%d", fired)
	}
}

// TestSourceStaleConcurrentFiresOnce 守并发防抖：chooseRoute 被 Tick 与回路 API
// 两个 goroutine 并发调用，多个 goroutine 同时命中 bindable=false 时只能通知一次。
// 裸 bool 会双通知（2026-11-24 trouble-maker 阻断项）。
func TestSourceStaleConcurrentFiresOnce(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	SetPhysicalSources(PhysicalSources{V4: v4})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	pl.pt.resolveFor = func(net.IP) (net.IP, error) { return net.ParseIP("1.2.3.4"), nil }
	pl.bindable = func(net.IP) bool { return false }

	var mu sync.Mutex
	fired := 0
	pl.sourceStale = func() {
		mu.Lock()
		fired++
		mu.Unlock()
	}
	const n = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			pl.chooseRoute(pl.portal, pl.systemPortal)
		}()
	}
	close(start)
	wg.Wait()
	if fired != 1 {
		t.Fatalf("并发 %d 次失效，SourceStale 触发 %d 次，期望 1 次（竞态/防抖失效）", n, fired)
	}
}

// TestInjectedEgressInvalidNotifiesSourceStale 守接口 C：注入模式（无 rd）下
// 出口绑定失效的最强信号不能丢——守护的 OnEgressInvalid 必须触发 SourceStale
// 通知 Kotlin 重注入。自动发现路径（有 rd）仍走原 picker.Down+rd.force，
// 本用例只覆盖注入分支（T4 守自动路径零回归）。
func TestInjectedEgressInvalidNotifiesSourceStale(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	SetPhysicalSources(PhysicalSources{V4: v4})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if pl.rd != nil {
		t.Fatal("注入模式不应装配 rediscoverer")
	}
	d := newDaemon(cfg, pl)
	fired := 0
	pl.sourceStale = func() { fired++ }

	if d.OnEgressInvalid == nil {
		t.Fatal("注入模式下 OnEgressInvalid 未装配——绑定失效信号被吞")
	}
	d.OnEgressInvalid()
	if fired != 1 {
		t.Fatalf("注入模式绑定失效未通知 SourceStale：fired=%d", fired)
	}
}

func TestV6RotationIsVisibleToSnapshot(t *testing.T) {
	v4 := net.ParseIP("10.20.30.40")
	first := net.ParseIP("2001:db8::1")
	rotated := net.ParseIP("2001:db8::2")
	SetPhysicalSources(PhysicalSources{V4: v4, V6: first})
	t.Cleanup(func() { SetPhysicalSources(PhysicalSources{}) })
	cfg := defaultAdvancedConfig()
	cfg.Server = "http://portal.example"
	pl, err := newPlumbing(cfg, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	d := newDaemon(cfg, pl)
	a := newAPI(&cfg, pl, d)

	// 临时地址轮换：Kotlin 重新注入，面板显示与握手取源都要跟着变。
	SetPhysicalSources(PhysicalSources{V4: v4, V6: rotated})
	if got := a.Snapshot().V6Src; got != rotated.String() {
		t.Errorf("轮换后面板仍显示旧地址 %q，期望 %q", got, rotated.String())
	}
	if got := v6Source(pl.currentV6); !got.Equal(rotated) {
		t.Errorf("轮换后握手源仍是 %v", got)
	}
}
