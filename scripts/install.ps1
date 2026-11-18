# cnal 标准安装：三个 exe 装到 %LOCALAPPDATA%\Programs\cnal\，并刷新
# 开始菜单快捷方式（cnalw 带面板）+ HKCU Run 自启（cnald 无头守护）。
# 重复运行即原地更新（覆盖 exe、重写快捷方式）。
#
# 用法：powershell -File install.ps1（源码仓库或解压后的发布 zip 均可）
#       [-SourceDir <自定义 exe 目录>]
param(
    [string]$SourceDir = ""
)
$ErrorActionPreference = "Stop"

# SourceDir 缺省自适应两种布局：发布 zip（脚本与 exe 同目录，Compress-Archive
# 会扁平化路径）与源码仓库（脚本在 scripts\，exe 在 ..\）。
if ($SourceDir -eq "") {
    if (Test-Path (Join-Path $PSScriptRoot "cnalw.exe")) {
        $SourceDir = $PSScriptRoot
    } else {
        $SourceDir = Join-Path $PSScriptRoot ".."
    }
}

$installDir = "$env:LOCALAPPDATA\Programs\cnal"
$configPath = "$env:LOCALAPPDATA\campus-net-autologin\config.ini"

# 1. 安装目录 + 复制 exe（cnal CLI / cnald 无头守护 / cnalw 托盘）。
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

# 1a. 子系统护栏（复制前验源）：cnalw 必须 GUI 子系统（2），否则开始菜单一点
# 就弹黑窗（2026-11-08 实证：手搓裸 go build 产出 console exe）。先验后拷，
# 拦截时不把脏 exe 拷进安装目录。
$b = [IO.File]::ReadAllBytes((Join-Path $SourceDir "cnalw.exe"))
$pe = [BitConverter]::ToInt32($b, 0x3C)
$subsys = [BitConverter]::ToUInt16($b, $pe + 0x5C)
if ($subsys -ne 2) {
    throw "源 cnalw.exe 子系统=$subsys（应 2=GUI）——用 build.ps1 构建（带 -H windowsgui），别裸 go build"
}

foreach ($exe in @("cnal.exe", "cnald.exe", "cnalw.exe")) {
    $src = Join-Path $SourceDir $exe
    if (-not (Test-Path $src)) { throw "缺 $src——先 go build ./cmd/..." }
    Copy-Item $src $installDir -Force
}
Write-Output "OK 三个 exe 已装到 $installDir（cnalw 子系统=2 GUI 已验）"

# 2. 开始菜单快捷方式总是创建：cnalw 首次运行会自动探测门户并打开配置面板
# （2026-11-08 首跑路径补强后不再"启动即报错退出"）。
$lnkPath = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\校园网守护.lnk"
$ws = (New-Object -ComObject WScript.Shell).CreateShortcut($lnkPath)
$ws.TargetPath = Join-Path $installDir "cnalw.exe"
$ws.Arguments = '--config "{0}"' -f $configPath
$ws.WorkingDirectory = $installDir
$ws.IconLocation = (Join-Path $installDir "cnalw.exe") + ",0"
$ws.Save()
Write-Output "OK 开始菜单快捷方式已指向 $installDir\cnalw.exe"

# 3. 自启只在配置就绪时登记：否则每次开机先弹首次配置窗口，体验倒挂。
$configReady = Test-Path $configPath
if (-not $configReady) {
    Write-Output "SKIP 未找到 $configPath——暂不登记自启。"
    Write-Output "     首次运行 cnalw 完成配置后，重跑本脚本或托盘勾选「开机自启」。"
} else {
    # HKCU Run 自启（换役 Python 守护：装完请退出旧托盘）。
    $runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
    $name = "CampusNetAutoLogin-Go"
    $value = '"{0}" --config "{1}"' -f (Join-Path $installDir "cnalw.exe"), $configPath
    Set-ItemProperty -Path $runKey -Name $name -Value $value -Type String
    $written = (Get-ItemProperty $runKey).$name
    if ($written -ne $value) { throw "写入校验失败：$written" }
    Write-Output "OK 自启已登记: $name"
}

Write-Output "换役提醒：Python 守护仍在跑时两者会并行——先退出 Python 托盘再登录。"
