package dev.cnal.m0

/** 候选网络：从 NetworkCapabilities 抽出的判定事实（抽出来才能纯函数测）。 */
data class NetCandidate(val isWifi: Boolean, val hasVpn: Boolean, val isValidated: Boolean)

/**
 * 选网（android-plan §4.2 第 1-3 步）：`TRANSPORT_WIFI` 且不含
 * `NET_CAPABILITY_VPN`。已验证（VALIDATED）的优先；一个都不合格就交白卷
 * 返回 -1——不绑就明说，绝不退回蜂窝瞎绑（A4 的整条结论都建在这一步上）。
 */
object NetPick {
    fun pickIndex(cands: List<NetCandidate>): Int {
        val eligible = cands.withIndex().filter { (_, c) -> c.isWifi && !c.hasVpn }
        if (eligible.isEmpty()) return -1
        return eligible.firstOrNull { (_, c) -> c.isValidated }?.index ?: eligible.first().index
    }
}
