//go:build windows

// Package store 凭据的家：Windows 凭据管理器直读写删（零依赖 advapi32，
// 与 Python keyring WinVaultKeyring 同存储形态：generic 凭据、simple target
// = 服务名、UTF-16LE blob、Persist=Local Machine——同机同用户可读）。
// 凭据只住凭据管理器，任何文件不得出现真实凭据（t41 系红线）。
package store

import (
	"errors"
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// credTypeGeneric = CRED_TYPE_GENERIC（Python keyring 写的就是这个类型）。
const credTypeGeneric = 1

// credPersistLocalMachine = CRED_PERSIST_LOCAL_MACHINE（keyring Persist 同值）。
const credPersistLocalMachine = 2

// _CREDENTIAL 结构（wincred.h CREDENTIALW）：字段顺序与对齐按 Win32 布局。
// 指针字段用 Go 指针类型（*uint16/*byte）——GC 可见、vet 合规，ABI 同形。
type _CREDENTIAL struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         *byte // Win32 为 pointer；本实现不使用属性
	TargetAlias        *uint16
	UserName           *uint16
}

// ReadCredential 读 generic 凭据，返回 (UserName, 密码明文, error)。
// 密码 blob 是 UTF-16LE（win32cred 传 str 的落盘形态）。
func ReadCredential(target string) (string, string, error) {
	tp, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return "", "", err
	}
	var cp *_CREDENTIAL
	r1, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(tp)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&cp)),
	)
	if r1 == 0 {
		return "", "", fmt.Errorf("凭据 %q 读取失败: %v（守护是否保存过？）", target, callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(cp)))
	user := utf16ToStr(cp.UserName)
	secret := ""
	if cp.CredentialBlobSize > 0 && cp.CredentialBlob != nil {
		secret = utf16BlobToStr(unsafe.Slice(cp.CredentialBlob, cp.CredentialBlobSize))
	}
	return user, secret, nil
}

// WriteCredential 写 generic 凭据（simple target，Persist=Local Machine，
// 与 keyring.set_password 落点一致——Python/Go 守护互认同一存储）。
func WriteCredential(target, user, secret string) error {
	// 空密码在入口就拒绝：写空 blob 等于覆盖已存的真密码（2026-11-08 发布
	// 审查 P0）。api 层已有一道拒绝，这里是 store 层的第二道。
	if secret == "" {
		return errors.New("空密码不写入凭据管理器（会覆盖已有凭据）")
	}
	tp, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	up, err := syscall.UTF16PtrFromString(user)
	if err != nil {
		return err
	}
	blob := utf16BlobFromStr(secret)
	sp, err := syscall.UTF16PtrFromString("Stored using python-keyring / cnal")
	if err != nil {
		return err
	}
	c := _CREDENTIAL{
		Flags:              0,
		Type:               credTypeGeneric,
		TargetName:         tp,
		Comment:            sp,
		CredentialBlobSize: uint32(len(blob)),
		Persist:            credPersistLocalMachine,
		UserName:           up,
	}
	// 入口已拒绝空密码；这里仍判空再取地址，防 &blob[0] 对空切片 panic
	// （2026-11-08 发布审查 P0 的第二道防线）。
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	r1, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r1 == 0 {
		return fmt.Errorf("凭据 %q 写入失败: %v", target, callErr)
	}
	return nil
}

// DeleteCredential 删除 generic 凭据（找不到也返回错误，调用方自行容忍）。
func DeleteCredential(target string) error {
	tp, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r1, _, callErr := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(tp)),
		credTypeGeneric,
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("凭据 %q 删除失败: %v", target, callErr)
	}
	return nil
}

// utf16ToStr 读 Win32 字符串指针。
func utf16ToStr(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; ptr = unsafe.Pointer(uintptr(ptr) + 2) {
		n++
	}
	return string(utf16.Decode(unsafe.Slice(p, n)))
}

// utf16BlobToStr 密码 blob（UTF-16LE 字节串）→ Go 字符串。
func utf16BlobToStr(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// utf16BlobFromStr Go 字符串 → UTF-16LE 字节串（写回落盘形态）。
func utf16BlobFromStr(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, v := range u {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	return b
}
