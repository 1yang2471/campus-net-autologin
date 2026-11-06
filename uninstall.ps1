# campus-net-autologin 卸载脚本（便携版=删本体+入口，绝不碰数据目录）
# 由「设置 → 应用 → 校园网守护 → 卸载」调用。遵循工作区破坏性操作守则：
# 先展示全部解析后路径与文件数 → 输入大写 DELETE 确认 → 才执行。
$ErrorActionPreference = 'Stop'

$exe      = 'D:\workspace\campus-net-autologin\dist\campus-net-autologin\campus-net-autologin.exe'
$body     = Split-Path $exe                    # 程序本体目录
$desktop  = 'C:\Users\abc\Desktop\校园网守护.lnk'
$programs = [Environment]::GetFolderPath('Programs')
$startLnk = Join-Path $programs '校园网守护.lnk'
$unReg    = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\campus-net-autologin'
$runKey   = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'

Write-Host '将删除（程序本体与入口，数据保留）:'
foreach ($p in @($body, $desktop, $startLnk, $unReg)) {
    if (Test-Path -LiteralPath $p) {
        if ((Get-Item -LiteralPath $p) -is [System.IO.DirectoryInfo]) {
            $n = (Get-ChildItem -LiteralPath $p -Recurse -File).Count
            $sz = [math]::Round(((Get-ChildItem -LiteralPath $p -Recurse -File |
                  Measure-Object Length -Sum).Sum / 1MB), 1)
            Write-Host ("  [目录] {0}  ({1} 个文件, {2} MB)" -f $p, $n, $sz)
        } else { Write-Host ("  [项]   {0}" -f $p) }
    } else { Write-Host ("  [跳过] {0}（不存在）" -f $p) }
}
Write-Host '保留不动: %LOCALAPPDATA%\campus-net-autologin（config.ini/keeper.log）'
Write-Host '保留不动: Windows 凭据管理器中的密码条目（可在凭据管理器自行删除）'

$answer = Read-Host '确认卸载请输入大写 DELETE（其他任意输入取消）'
if ($answer -cne 'DELETE') { Write-Host '已取消，什么都没做。'; exit 0 }

Stop-Process -Name campus-net-autologin -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
# 硬护栏：只允许删上述四个精确路径（防路径解析被劫持）
foreach ($p in @($body, $desktop, $startLnk)) {
    if (Test-Path -LiteralPath $p) {
        if ($p -notin @($body, $desktop, $startLnk)) { throw "护栏拒绝: $p" }
        Remove-Item -LiteralPath $p -Recurse -Force
    }
}
if (Test-Path $unReg) { Remove-Item -LiteralPath $unReg -Recurse -Force }
if (Test-Path $runKey) {
    $v = (Get-ItemProperty -Path $runKey).PSObject.Properties.Name |
         Where-Object { $_ -eq 'campus-net-autologin' }
    if ($v) { Remove-ItemProperty -Path $runKey -Name 'campus-net-autologin' }
}
Write-Host '卸载完成。数据目录 %LOCALAPPDATA%\campus-net-autologin 已保留。'
