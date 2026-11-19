package dev.cnal.m0

/**
 * 校园 DNS 自动注入的纯逻辑（真机实证 2026-11-19：新装 app 的 config.ini 里
 * `campus_dns` 只是一行注释提示，用户不得不从 PC 抄一个值——但 DHCP 早就把它
 * 下发了，就在绑定 Wi-Fi 的 LinkProperties.dnsServers 里）。
 *
 * 判据：
 * 1. 只挑一个值（内核的 campus_dns 是单值，校事实）；
 * 2. 优先 IPv4——门户域名解析/钉 IP 走物理栈，v4 是 v1 承诺面；
 * 3. 已有生效值绝不覆盖（用户手改优先；面板设置里能看到自动填的结果）。
 */
object CampusDns {
    /** 从 DNS 列表挑注入值；空列表返回空串（= 不注入，保持原配置）。 */
    fun pick(dns: List<String>): String {
        val ips = dns.map { it.trim() }.filter { it.isNotEmpty() }
        return ips.firstOrNull { !it.contains(':') } ?: ips.firstOrNull().orEmpty()
    }

    /**
     * 确保 ini 行里 `campus_dns` 有生效值。返回新行列表；与输入相等 = 无需写盘。
     *
     * 查找顺序：生效行（非注释）→ 内置占位注释行 → [keeper] 段首 → 末尾追加。
     * 生效行已有非空值原样保留；生效行为空则填上。
     */
    fun ensure(lines: List<String>, dns: String): List<String> {
        if (dns.isBlank()) return lines
        fun isActive(line: String): Boolean {
            val t = line.trimStart()
            return t.startsWith("campus_dns") && !t.startsWith(";") && !t.startsWith("#")
        }
        val activeIdx = lines.indexOfFirst { isActive(it) }
        if (activeIdx >= 0) {
            // 已有非空值：尊重用户/面板手改，不动。
            if (lines[activeIdx].substringAfter("=", "").trim().isNotEmpty()) return lines
            val out = lines.toMutableList()
            out[activeIdx] = "campus_dns = $dns"
            return out
        }
        val placeholderIdx = lines.indexOfFirst {
            val t = it.trimStart()
            t.startsWith(";") && t.contains("campus_dns")
        }
        val out = lines.toMutableList()
        if (placeholderIdx >= 0) {
            out[placeholderIdx] = "campus_dns = $dns"
            return out
        }
        val keeperIdx = out.indexOfFirst { it.trim().equals("[keeper]", ignoreCase = true) }
        if (keeperIdx >= 0) out.add(keeperIdx + 1, "campus_dns = $dns") else out.add("campus_dns = $dns")
        return out
    }
}
