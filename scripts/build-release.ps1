# Release 签名构建：口令从 Windows Credential Manager 读，只在本进程内存里，
# 不落文件、不进命令行日志。keystore 在仓库外（默认 D:\workspace\secrets\cnal-release.jks）。
# 用法：powershell -ExecutionPolicy Bypass -File scripts/build-release.ps1
$ErrorActionPreference = 'Stop'

# cmdkey 不提供读回密码的 API，用 Windows Credential API 从 keyring 取。
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class CredRead {
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    public struct CREDENTIAL { public uint Flags; public string TargetName; public string Comment;
        public System.Runtime.InteropServices.ComTypes.FILETIME LastWritten; public uint CredentialBlobSize;
        public IntPtr CredentialBlob; public uint Persist; public uint AttributeCount; public IntPtr Attributes;
        public IntPtr TargetAlias; public string UserName; }
    [DllImport("advapi32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern bool CredReadW(string target, uint type, uint flags, out IntPtr cred);
    [DllImport("advapi32.dll")] public static extern void CredFree(IntPtr cred);
}
"@
$ptr = [IntPtr]::Zero
if (-not [CredRead]::CredReadW('CNAL_SIGN_KEY', 1, 0, [ref]$ptr)) {
    throw "keyring 无 CNAL_SIGN_KEY 条目：先用 keytool 生成密钥并把口令存进 Windows Credential Manager"
}
$cred = [Runtime.InteropServices.Marshal]::PtrToStructure($ptr, [type][CredRead+CREDENTIAL])
$blob = [Runtime.InteropServices.Marshal]::PtrToStringUni($cred.CredentialBlob, [int]($cred.CredentialBlobSize / 2))
[CredRead]::CredFree($ptr)

# 显式护栏：release 入口必须有 keyring 口令 + keystore 文件，缺一即失败
# （build.gradle 不在配置期抛异常，否则 debug 测试也被挡；护栏集中在这个唯一入口）
if (-not (Test-Path 'D:\workspace\secrets\cnal-release.jks')) {
    throw "keystore 不存在：D:\workspace\secrets\cnal-release.jks"
}
$env:CNAL_SIGN_STORE = 'D:\workspace\secrets\cnal-release.jks'
$env:CNAL_SIGN_STOREPASS = $blob
$env:CNAL_SIGN_KEYPASS = $blob

Push-Location (Join-Path $PSScriptRoot '..')
try {
    # 不 --offline：release 的 lintVital 任务需联网取 lint-gradle（首次构建一次即可，之后走缓存）
    & .\gradlew :app:assembleRelease
    if ($LASTEXITCODE -ne 0) { throw "release 构建失败" }
    # 签名产物是 app-release.apk（gradle 签名后覆盖，不是 -unsigned）
    $apk = 'app\build\outputs\apk\release\app-release.apk'
    Write-Output "Release APK: $apk"
    # apksigner 验签主体（不输出口令）
    $sdk = "${env:ANDROID_HOME}"
    if (-not $sdk) { $sdk = 'D:\Android\sdk' }
    & "$sdk\build-tools\35.0.0\apksigner.bat" verify --print-certs $apk
} finally {
    Pop-Location
    Remove-Variable blob -ErrorAction SilentlyContinue
}
