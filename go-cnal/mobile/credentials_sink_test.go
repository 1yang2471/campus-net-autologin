package mobile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cnal/internal/app"
)

// 安卓凭据落盘缝：面板保存的账号必须回到 Kotlin Keystore，否则面板里新增/
// 修改的账号只活在 Go 内存，进程重启就没了（多账号切换直接坏掉）。
//
// 两条路径必须分开：
//   - creds.Save（面板 POST /config → app.SaveCredentials → CredentialSource.Save）
//     先落 Kotlin，再进内存；落盘失败 → 整个保存失败，内存不得半写。
//   - SetCredentials（启动时 Kotlin 注入）只进内存，不再回调 Kotlin——否则
//     每次启动都把全部账号重加密一遍，且回调链更深。

type sinkFunc func(user, pass string) error

func (f sinkFunc) Save(user, pass string) error { return f(user, pass) }

func TestCredentialSinkPersistsPanelSave(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { SetCredentialSink(nil); resetState(t) })
	var got []string
	SetCredentialSink(sinkFunc(func(user, pass string) error {
		got = append(got, user+"="+pass)
		return nil
	}))
	if err := creds.Save("sink-alice", "pw-a"); err != nil {
		t.Fatalf("Save 不应失败: %v", err)
	}
	if len(got) != 1 || got[0] != "sink-alice=pw-a" {
		t.Fatalf("落盘缝没被调用: %#v", got)
	}
	if p, err := creds.Load("sink-alice"); err != nil || p != "pw-a" {
		t.Fatalf("内存也未写入: p=%q err=%v", p, err)
	}
}

func TestCredentialSinkErrorLeavesMemoryUntouched(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { SetCredentialSink(nil); resetState(t) })
	SetCredentialSink(sinkFunc(func(string, string) error {
		return errors.New("keystore unavailable")
	}))
	if err := creds.Save("sink-bob", "pw-b"); err == nil {
		t.Fatal("落盘失败必须向上报错，不能假装保存成功")
	}
	if _, err := creds.Load("sink-bob"); err == nil {
		t.Fatal("落盘失败时内存不得半写（否则本次运行能用、重启就丢）")
	}
}

// SourceSink 通知 Kotlin 重新注入物理源（D-09）：绑定失效时 Go 侧只发信号，
// 真正的 LinkProperties 重取在 Kotlin。这里验证 sink 转发到位（对称 CredentialSink）。
type sourceSinkFunc func()

func (f sourceSinkFunc) SourceStale() { f() }

func TestSourceSinkNotifiesOnStale(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { SetSourceSink(nil); resetState(t) })
	calls := 0
	SetSourceSink(sourceSinkFunc(func() { calls++ }))

	// 经 app 层包级通知入口触发（与 plumbing.notifySourceStale 同一条路径）。
	app.NotifySourceStale()
	if calls != 1 {
		t.Fatalf("SourceStale 未通知 Kotlin：calls=%d", calls)
	}
}

func TestSetCredentialsBypassesSink(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { SetCredentialSink(nil); resetState(t) })
	calls := 0
	SetCredentialSink(sinkFunc(func(string, string) error {
		calls++
		return nil
	}))
	if err := SetCredentials("sink-carol", "pw-c"); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("启动注入不得回调 Keystore（重复加密/回调链）: calls=%d", calls)
	}
}

// 安卓首跑 ini 只有 server/login_path：不补种子，面板「历史账号」是空下拉，
// /config 的 username 也是空（M1 首跑现场已见）。Start 必须幂等补齐。
func TestStartSeedsAccountHistory(t *testing.T) {
	resetState(t)
	t.Cleanup(func() { resetState(t); app.SetDataDir("") })
	dir := t.TempDir()
	SetDataDir(dir)
	if err := SetCredentials("alice", "pw-a"); err != nil {
		t.Fatal(err)
	}
	if err := SetPhysicalSources("10.20.30.40", ""); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(dir, "config.ini")
	body := "[keeper]\nserver = http://portal.example\nlogin_path = /ac_portal/login.php\n" +
		"interval = 3600\nprobe_timeout = 1\n"
	if err := os.WriteFile(ini, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(ini); err != nil {
		t.Fatalf("Start 应能装配内核：%v", err)
	}
	defer Stop()
	b, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "username = alice") {
		t.Errorf("ini 缺账号指向源: %s", s)
	}
	if !strings.Contains(s, "[accounts]") || !strings.Contains(s, "1 = alice|http://portal.example") {
		t.Errorf("ini 缺历史账号种子: %s", s)
	}
}
