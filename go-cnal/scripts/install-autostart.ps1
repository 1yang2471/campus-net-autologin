# cnalw 开机自启安装：HKCU Run 键（仅当前用户，无服务/无管理员）。
# 用法：powershell -File scripts\install-autostart.ps1 [-ExePath D:\workspace\go-cnal\cnalw.exe] [-ConfigPath <守护 ini>]
param(
    [string]$ExePath = (Join-Path $PSScriptRoot "..\cnalw.exe"),
    [string]$ConfigPath = "$env:LOCALAPPDATA\campus-net-autologin\config.ini"
)
$ErrorActionPreference = "Stop"

if (-not (Test-Path $ExePath)) { throw "缺 $ExePath——先 go build -o cnalw.exe ./cmd/cnalw" }
if (-not (Test-Path $ConfigPath)) { throw "缺 $ConfigPath" }

$runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$name = "CampusNetAutoLogin-Go"
$value = '"{0}" --config "{1}"' -f (Resolve-Path $ExePath).Path, (Resolve-Path $ConfigPath).Path

Set-ItemProperty -Path $runKey -Name $name -Value $value -Type String
$written = (Get-ItemProperty $runKey).$name
if ($written -ne $value) { throw "写入校验失败：$written" }
Write-Output "OK 自启已登记: $name = $value"
Write-Output "换役提醒：Python 守护仍在服役时，两者会并行——切换前先退出 Python 守护（托盘右键退出）。"
