package dev.cnal.m0

/**
 * 网络不适合校园认证时的暂停守卫（2026-11-18：VPN → 再扩到蜂窝直连/热点）。
 *
 * 纯函数，不碰 Android API：KeeperService 的 NetworkCallback 读出两个系统事实
 * 喂进来——默认网络是否带 VPN、是否只剩蜂窝（开热点/连不上校园 Wi-Fi 时）。
 * 与用户原本的 auto_reconnect 设定一起算边沿动作。
 *
 * 规则：
 * - VPN 出现 → 暂停自动登录（探测展示照常，不登录，避免被 TUN 抓走误判掉线）；
 * - 默认网络只剩蜂窝（开热点 AP 时手机通常掉 Wi-Fi）→ 暂停，避免用蜂窝撞门户；
 * - 两者都消失 → 立即恢复 + 通知；重复回调不重复动作；
 * - 用户原本就关着 auto_reconnect → 全程不动。
 */
class VpnGuard {
    enum class Action { NONE, PAUSE, RESUME }

    private var unsuitable: Boolean? = null // null = 还没建立基线
    private var lastPauseReason: String = ""

    /**
     * @param vpnPresent 默认网络带 TRANSPORT_VPN
     * @param cellularOnly 默认网络只剩蜂窝（无 Wi-Fi/以太网；开热点 AP 的典型形态）
     */
    fun onNetworkChanged(
        vpnPresent: Boolean,
        cellularOnly: Boolean,
        userAutoReconnect: Boolean,
    ): Action {
        val now = vpnPresent || cellularOnly
        if (now) {
            lastPauseReason = if (vpnPresent) "vpn" else "cellular"
        }
        val previous = this.unsuitable
        this.unsuitable = now
        if (!userAutoReconnect) return Action.NONE // 用户自己关的，不替他打开
        if (previous == now) return Action.NONE // 边沿：只在状态翻转时动作
        return if (now) Action.PAUSE else if (previous == true) Action.RESUME else Action.NONE
    }

    /** 最近一次暂停的原因："vpn" 或 "cellular"（恢复通知沿用）。 */
    fun pauseReason(): String = lastPauseReason
}
