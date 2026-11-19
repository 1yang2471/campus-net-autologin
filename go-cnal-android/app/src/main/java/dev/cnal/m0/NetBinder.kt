package dev.cnal.m0

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities

/**
 * 选网 + `bindProcessToNetwork`（android-plan §4.2）。选择规则在 [NetPick]
 * ——A4「绑 Wi-Fi 后出口是否仍走校园网」整条结论都建立在这一步上，所以判定
 * 逻辑必须是纯函数、可单测，不看框架。
 *
 * 一个都不合格时**解除绑定**并如实报告「未绑定」：不绑就明说，绝不退回蜂窝
 * 瞎绑（那会让 A4 的出口数字变成假证据）。
 */
class NetBinder(context: Context) {
    private val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager

    data class Bound(
        val bound: Boolean, val label: String, val v4: String, val v6: String,
        // 该网络全部 v6 地址（保序）：第一个 v6 常是 fe80 链路本地，GUA 在
        // 后面——v6 注入要从全列表挑，不能只看 v6 一个（2026-11-12 真机实证）。
        val v6All: List<String> = emptyList(),
        // 该链路 DHCP/RDNSS 下发的 DNS 服务器（校园 DNS 自动注入的原料：
        // 新装机不必再从 PC 手抄 campus_dns，2026-11-19）。
        val dns: List<String> = emptyList(),
    )

    fun bind(): Bound {
        val nets = cm.allNetworks.toList()
        val idx = NetPick.pickIndex(nets.map { candidate(it) })
        if (idx < 0) {
            cm.bindProcessToNetwork(null)
            return Bound(false, "未找到 Wi-Fi（或已带 VPN）——未绑定", "—", "—")
        }
        val net = nets[idx]
        val ok = cm.bindProcessToNetwork(net)
        val lp = cm.getLinkProperties(net)
        return Bound(ok, label(net), addrOf(lp, 4), addrOf(lp, 6), v6AllOf(lp), dnsOf(lp))
    }

    private fun candidate(net: Network): NetCandidate {
        val caps = cm.getNetworkCapabilities(net)
        return NetCandidate(
            isWifi = caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true,
            // 没有公开的 NET_CAPABILITY_VPN：VPN 用 TRANSPORT_VPN 标识，且系统会
            // **清掉** NET_CAPABILITY_NOT_VPN。两条都看，缺 NOT_VPN 就按 VPN 处理
            // （fail-closed：宁可报"未绑定"，也不把出口不明的网当成校园网）。
            hasVpn = caps?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true ||
                caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN) != true,
            isValidated = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true,
        )
    }

    private fun label(net: Network): String {
        val caps = cm.getNetworkCapabilities(net)
        val validated = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true
        return if (validated) "Wi-Fi 已绑定（已验证）" else "Wi-Fi 已绑定（未验证）"
    }

    /** 取该链路第一个同族地址（展示用：证明绑到了哪张网，不证明出口）。 */
    private fun addrOf(lp: LinkProperties?, family: Int): String {
        val want = if (family == 4) 4 else 16
        lp?.linkAddresses?.forEach { la ->
            if (la.address.address.size == want) {
                return la.address.hostAddress + "/" + la.prefixLength
            }
        }
        return "—"
    }

    /** 该链路的 DNS 服务器（v4/v6 都收；挑哪个由 CampusDns.pick 决定）。 */
    private fun dnsOf(lp: LinkProperties?): List<String> =
        lp?.dnsServers?.mapNotNull { it.hostAddress } ?: emptyList()

    /** 该链路全部 v6 地址（保序，带 /prefix；挑 GUA 的原料，见 Bound.v6All）。 */
    private fun v6AllOf(lp: LinkProperties?): List<String> {
        val out = mutableListOf<String>()
        lp?.linkAddresses?.forEach { la ->
            if (la.address.address.size == 16) {
                out += la.address.hostAddress + "/" + la.prefixLength
            }
        }
        return out
    }
}
