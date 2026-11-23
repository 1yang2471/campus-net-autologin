# 通用「系统 DNS 缓存残留 fake-ip」诊断 + 修复（2026-11-23）。
#
# 现象：开关代理/TUN 后，少数网站能开、大多数打不开，过几分钟自愈。根因是
# fake-ip 映射表在 TUN 重建时丢失，但系统 DNS 缓存里还留着假 IP（28.0.0.x /
# 198.18.x），浏览器继续连这些虚构地址就失败，等缓存过期才恢复。
#
# 通用性：只认 fake-ip 保留段这一环境事实（Clash/Mihomo 及一切 TUN 代理通用），
# 不点名任何具体软件、进程、端口。守护走物理栈不受影响，本脚本只处理浏览器
# 那层系统 DNS。
#
# 重要边界（见 docs/adr/0001）：TUN 正常开着时系统 DNS 本就返回假 IP，单看
# 新查询结果无法判定故障。本脚本以「缓存快照」为准（在任何新查询之前读取，
# 避免脚本自身查询污染），结论用「疑似」，由用户结合体感判断。
#
# fake-ip 段与 Go 侧 internal/netstack/excludedSrcNets() 同源（28.0.0.0/8、
# 198.18.0.0/15）；两侧改动须同步。
#
# 用法（只读诊断，不改任何东西）：
#   powershell -File diag-dns-hijack.ps1
# 修复（清系统 DNS 缓存，秒级恢复，不改网卡 DNS/代理设置）：
#   powershell -File diag-dns-hijack.ps1 -Fix
param([switch]$Fix)

$fakePrefixes = @('28.', '198.18.', '198.19.')

function Test-FakeIP {
    param([string]$ip)
    if (-not $ip) { return $false }
    foreach ($p in $fakePrefixes) { if ($ip.StartsWith($p)) { return $true } }
    return $false
}

Write-Output "===== DNS 假 IP 残留诊断  $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') ====="
Write-Output ""

# 1. 缓存快照 MUST 在任何 Resolve-DnsName 之前读——脚本自己的查询会把答案
#    写进缓存，之后再读就会把「脚本刚造的缓存」误当成「故障残留」。
Write-Output "--- [1] 系统 DNS 缓存快照（故障现场；在本次任何查询之前读取）---"
$cacheErr = $null
$cache = @(Get-DnsClientCache -ErrorVariable cacheErr -ErrorAction SilentlyContinue)
$cacheReadOK = ($cacheErr.Count -eq 0)
if (-not $cacheReadOK) {
    Write-Output ("  [警告] 无法读取系统 DNS 缓存（{0}），本节与结论不可信。" -f $cacheErr[0].Exception.Message)
}
$fakeEntries = @($cache | Where-Object { Test-FakeIP ([string]$_.Data) })
Write-Output ("  缓存总数: {0}  假 IP 条目: {1}" -f $cache.Count, $fakeEntries.Count)
$fakeEntries | Select-Object -First 15 | ForEach-Object {
    Write-Output ("    {0,-40} -> {1}" -f $_.Name, $_.Data)
}

# 2. 网卡 DNS 配置（判断是否被代理软件改成 127.0.0.1:1053 之类）。
Write-Output ""
Write-Output "--- [2] 各网卡 DNS 服务器 ---"
Get-DnsClientServerAddress -AddressFamily IPv4 | Where-Object { $_.ServerAddresses.Count -gt 0 } | ForEach-Object {
    Write-Output ("  {0,-14}: {1}" -f $_.InterfaceAlias, ($_.ServerAddresses -join ', '))
}

# 3. 结论（措辞收敛为「疑似」，由用户结合体感判断）。
Write-Output ""
Write-Output "===== 结论 ====="
if ($fakeEntries.Count -gt 0) {
    Write-Output "  [疑似] 系统 DNS 缓存里有 fake-ip 残留。若你此刻确实『少数网站能开、多数打不开、过段时间自愈』，这很可能就是原因——刷新 DNS 缓存或重连代理即可恢复。"
    Write-Output "  注意：TUN 正常开着时系统 DNS 本就返回假 IP，单看假 IP 不是故障；守护认证走物理栈不受影响（面板在线是真的），问题只在浏览器系统 DNS 这层。"
    if ($Fix) {
        Write-Output ""
        Write-Output "  [修复] 清理系统 DNS 缓存..."
        try {
            Clear-DnsClientCache -ErrorAction Stop
            Write-Output "  系统 DNS 缓存已清空（通常立即可恢复）。"
        } catch {
            Write-Output ("  [失败] 清缓存未成功: {0}（可尝试以管理员身份重跑）" -f $_.Exception.Message)
        }
        Write-Output "  若浏览器仍异常：请重启浏览器（Chrome/Edge 有自己的 DNS 缓存），"
        Write-Output "  或重连一次代理/TUN 让 fake-ip 映射表重建。"
        Start-Sleep -Milliseconds 500
        $afterErr = $null
        $after = @(Get-DnsClientCache -ErrorVariable afterErr -ErrorAction SilentlyContinue | Where-Object { Test-FakeIP ([string]$_.Data) })
        if ($afterErr.Count -gt 0) {
            Write-Output "  复检失败：无法读取缓存，请人工确认。"
        } elseif ($after.Count -gt 0) {
            Write-Output ("  复检仍有 {0} 条假 IP —— 可能代理正在运行并重新填充缓存（属正常），以浏览器是否恢复为准。" -f $after.Count)
        } else {
            Write-Output "  复检：假 IP 条目已清零。"
        }
    } else {
        Write-Output ""
        Write-Output "  修复方法：powershell -File diag-dns-hijack.ps1 -Fix"
        Write-Output "  （清系统 DNS 缓存；不改网卡 DNS、不动代理设置；随时可重跑本脚本复查）"
    }
} elseif (-not $cacheReadOK) {
    Write-Output "  [无法判定] 系统 DNS 缓存读取失败——本次诊断无效，请检查权限后重跑。"
} else {
    Write-Output "  [未发现残留] 当前缓存无假 IP。若当时确实打不开网页，"
    Write-Output "  可能是真·出口受限（L2）或校园 DNS 异常——请看守护面板的状态与原因行；"
    Write-Output "  也可能是假 IP 已过期（问题已自愈），此时缓存视角看不到当时状态。"
}
Write-Output "===== 结束 ====="
