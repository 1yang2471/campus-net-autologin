package dev.cnal.m0

/**
 * 注入给 Go 内核的物理源（android-plan §3.4）。纯 Kotlin、不依赖 Android
 * 框架，便于 JVM 单测守两条判据：
 *
 * 1. 只给纯 IP，不带 LinkProperties 的 `/prefix`；
 * 2. v6 只取全局 GUA（2000::/3）——M0 实测模拟器只有 `fe80::` 链路本地，
 *    照抄"第一个 v6 地址"会把链路本地当 GUA 注进去，握手恒失败。
 *    蜂窝 rmnet 也可能带全局 v6，所以调用方只应传 wlan0 的地址列表。
 */
object PhysicalSource {
    /** 剥掉 CIDR 后缀；已是纯 IP 或空/占位符时原样/返回空。 */
    fun ip(withPrefix: String): String {
        val s = withPrefix.trim()
        if (s.isEmpty() || s == "—") return ""
        return s.substringBefore('/')
    }

    /**
     * 从 wlan0 的地址列表里挑第一个全局 GUA（2000::/3）。
     * 排除：`fe80::/10` 链路本地、`fc00::/7` 唯一本地、`::1` 环回、
     * v4（内核侧另走 v4 注入）。取不到返回 null = v1 只承诺 v4。
     */
    fun v6(addresses: List<String>): String? {
        for (raw in addresses) {
            val s = ip(raw)
            if (s.isEmpty() || !s.contains(':')) continue // v4 或占位
            val first = s.substringBefore(':').lowercase()
            if (first.isEmpty()) continue
            // 2000::/3：首段（16 位）落在 0x2000..0x3fff。之前误按"首字节
            // 0x20..0x3f"判，把 2001:db8::1 这样的真 GUA 也拒了。
            val group = first.toIntOrNull(16) ?: continue
            if (group !in 0x2000..0x3fff) continue
            if (s == "::1" || s.startsWith("fe80:") || s.startsWith("fc") ||
                s.startsWith("fd")) continue
            return s
        }
        return null
    }
}
