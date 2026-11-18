package dev.cnal.m0

/**
 * 探针锚点闸：只允许指向"真实互联网"的公网主机。
 *
 * 两个理由，缺一不可：
 * 1. 语义——锚点若落在私网/环回，探针通了也证明不了"到达真实互联网"，
 *    在线率就是假的；
 * 2. 安全——私网/保留地址（尤其 169.254.169.254 云元数据）是 SSRF 的经典
 *    落点，锚点地址在 M1 会变成可配置项。
 *
 * 域名不做解析（M0 不引入 DNS 依赖）：只做形态闸 + 字面量网段判定；真正的
 * 出口正确性由 A4 真机看门户侧出口 IP 决定，本闸不冒充那个结论。
 */
object AnchorPolicy {
    private val IPV4 = Regex("""^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$""")

    fun allows(host: String?): Boolean {
        val h = host?.trim()?.lowercase().orEmpty()
        if (h.isEmpty()) return false
        if (h == "localhost" || h.endsWith(".localhost")) return false

        val v4 = IPV4.matchEntire(h)
        if (v4 != null) {
            val o = v4.groupValues.drop(1).map { it.toIntOrNull() ?: return false }
            if (o.any { it > 255 }) return false // 非法点分：不放行成域名
            return isPublicV4(o[0], o[1])
        }
        if (h.contains(":")) return isPublicV6(h)
        // 其余按域名放行（形态闸；不做 DNS 解析）
        return h.contains(".")
    }

    private fun isPublicV4(a: Int, b: Int): Boolean = when {
        a == 0 || a == 10 || a == 127 -> false       // 本网/私网/环回
        a == 100 && b in 64..127 -> false            // CGNAT 100.64/10
        a == 169 && b == 254 -> false                // 链路本地 + 云元数据
        a == 172 && b in 16..31 -> false             // 172.16/12（不是整个 172/8）
        a == 192 && b == 168 -> false                // 192.168/16
        a >= 224 -> false                            // 组播/保留/255.255.255.255
        else -> true
    }

    private fun isPublicV6(host: String): Boolean {
        val h = host.removePrefix("[").removeSuffix("]")
        if (h.isEmpty() || h == "::1" || h == "::") return false
        // 只看首组 16 位：fe80::/10 链路本地、fc00::/7 唯一本地（含 fd00::/8）。
        val head = h.substringBefore(':').ifEmpty { "0" }.toIntOrNull(16) ?: return false
        val top = head ushr 8
        return top != 0xFE && top != 0xFC && top != 0xFD
    }
}
