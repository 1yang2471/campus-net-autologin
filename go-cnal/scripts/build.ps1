# 打包脚本更新：cnalw 链接图标资源（syso 随 go build 自动生效）。
param([string]$Version = "1.0.0")
$ErrorActionPreference = "Stop"

$go = "C:\Program Files\Go\bin\go.exe"
if (-not (Test-Path $go)) { $go = "go" }

$repo = Resolve-Path (Join-Path $PSScriptRoot "..")
Push-Location $repo
try {
    & $go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet 未通过" }
    & $go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test 未通过" }

    # 图标资源随源码改动再生成（app.ico 已入库时跳过）。
    if (-not (Test-Path "cmd\cnalw\app.ico")) {
        & $go run ./cmd/cnalw/genicon
    }

    New-Item -ItemType Directory -Force -Path dist | Out-Null
    # 先清旧产物：前两条构建失败时不得把上一版 exe 静默打进发布包
    # （2026-11-08 发布审查 P1）。
    Remove-Item -Force -ErrorAction SilentlyContinue cnal.exe, cnald.exe, cnalw.exe
    # -s -w 剥符号表与 DWARF（小而精：三 exe 共省 ~11MB；发布物不需要调试符号，
    # 崩溃定位走状态日志与 release_verify）。
    & $go build -ldflags "-s -w" -o cnal.exe  ./cmd/cnal
    if ($LASTEXITCODE -ne 0) { throw "cnal.exe 构建失败" }
    & $go build -ldflags "-s -w" -o cnald.exe ./cmd/cnald
    if ($LASTEXITCODE -ne 0) { throw "cnald.exe 构建失败" }
    & $go build -ldflags "-H windowsgui -s -w" -o cnalw.exe ./cmd/cnalw
    if ($LASTEXITCODE -ne 0) { throw "cnalw.exe 构建失败" }

    $zip = "dist\go-cnal-$Version-win64.zip"
    # 发布包必须自带正式安装脚本（装到 %LOCALAPPDATA%\Programs\cnal + 开始菜单
    # + 自启），否则用户拿到 zip 只能手搓散 exe（2026-11-08 发布审查实证缺口）。
    # 同时带 DESIGN.md 与 AGENTS.md 供二次开发对齐取值与踩坑清单。
    $pkg = @(
        "cnal.exe", "cnald.exe", "cnalw.exe",
        "scripts\install.ps1", "scripts\install-autostart.ps1", "scripts\remove-autostart.ps1",
        "scripts\release_verify.py", "README.md", "DESIGN.md", "AGENTS.md"
    )
    Compress-Archive -Path $pkg -DestinationPath $zip -Force
    $zipFile = Get-Item $zip
    Write-Output ("OK: {0}  ({1:N1} KB)" -f $zipFile.FullName, ($zipFile.Length / 1KB))
} finally {
    Pop-Location
}
