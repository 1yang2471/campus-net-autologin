# 一键更新安装版：构建（vet + test + 三 exe）→ 装到 %LOCALAPPDATA%\Programs\cnal。
#
# 与 install.ps1 的区别：正在运行的 cnalw/cnald 不杀进程——Windows 允许重命名
# 运行中的 exe（2026-11-10 实测），把被锁的旧文件改成 .old、新文件落位，下次
# 启动即用新版。默认不打断用户网络；-Restart 才强制重启托盘（会短暂断网）。
#
# 用法：
#   powershell -File scripts\update.ps1            # 构建 + 安装，运行中下次启动生效
#   powershell -File scripts\update.ps1 -Restart   # 构建 + 安装 + 立即重启 cnalw
param([switch]$Restart)
$ErrorActionPreference = "Stop"

$repo = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$installDir = "$env:LOCALAPPDATA\Programs\cnal"
$configPath = "$env:LOCALAPPDATA\campus-net-autologin\config.ini"
$exeNames = @("cnal.exe", "cnald.exe", "cnalw.exe")

# 运行中的 exe 不能覆盖，但可以改名；新文件落位后下次启动生效。
function Rename-RunningExe([string]$Path) {
    $old = "$Path.old"
    Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
    try {
        Move-Item -LiteralPath $Path -Destination $old -Force -ErrorAction Stop
        Write-Output "NOTE 运行中的 $Path 已改名为 .old；新版本下次启动生效"
        return $true
    } catch {
        Write-Warning "无法改名 $Path（$($_.Exception.Message)）；请退出托盘后重跑"
        return $false
    }
}

Push-Location $repo
try {
    # 运行中的进程路径（仓库根 / 安装目录两种启动方式都要覆盖）。
    $runningPaths = @(Get-Process cnalw, cnald -ErrorAction SilentlyContinue |
        ForEach-Object { try { $_.Path } catch { $null } } |
        Where-Object { $_ })

    # 1. 仓库根目录的 exe 若正被运行，build.ps1 的 Remove-Item 会失败——先让位。
    foreach ($name in $exeNames) {
        $rootExe = Join-Path $repo $name
        if ($runningPaths -contains $rootExe) { Rename-RunningExe $rootExe | Out-Null }
    }

    # 2. 构建（build.ps1 内含 go vet / go test；失败即抛，不装半成品）。
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "build.ps1")
    if ($LASTEXITCODE -ne 0) { throw "build.ps1 失败（exit=$LASTEXITCODE）" }

    # 3. 安装目录里正被运行的文件先改名（不杀进程）。
    foreach ($name in $exeNames) {
        $installedExe = Join-Path $installDir $name
        if ($runningPaths -contains $installedExe) { Rename-RunningExe $installedExe | Out-Null }
    }

    # 4. 安装 + 刷新快捷方式/自启（install.ps1 幂等，可重复执行）。
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot "install.ps1")
    if ($LASTEXITCODE -ne 0) { throw "install.ps1 失败（exit=$LASTEXITCODE）" }

    # 5. 哈希验收：装进去的必须就是刚构建的。
    foreach ($name in $exeNames) {
        $built = (Get-FileHash -LiteralPath (Join-Path $repo $name) -Algorithm SHA256).Hash
        $installed = (Get-FileHash -LiteralPath (Join-Path $installDir $name) -Algorithm SHA256).Hash
        if ($built -ne $installed) { throw "安装校验失败：$name 哈希不一致" }
    }
    Write-Output "OK 最新构建已装到 $installDir（正在运行的托盘需重启后生效）"

    # 6. 显式要求才重启；默认不打断用户网络。
    if ($Restart) {
        Get-Process cnalw, cnald -ErrorAction SilentlyContinue | Stop-Process -Force
        Start-Sleep -Milliseconds 500
        Start-Process -FilePath (Join-Path $installDir "cnalw.exe") `
            -ArgumentList '--config', "`"$configPath`""
        Write-Output "OK cnalw 已用新版本重启"
    }
} finally {
    Pop-Location
}
