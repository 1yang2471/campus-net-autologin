# 移除 cnalw 开机自启（与 install-autostart.ps1 对称）。
param()
$ErrorActionPreference = "Stop"
$runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$name = "CampusNetAutoLogin-Go"

if (Get-ItemProperty $runKey -Name $name -ErrorAction SilentlyContinue) {
    Remove-ItemProperty -Path $runKey -Name $name
    Write-Output "OK 已移除自启: $name"
} else {
    Write-Output "无自启项（本来就未安装）"
}
