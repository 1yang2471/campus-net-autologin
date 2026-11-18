package dev.cnal.m0

/**
 * VPN 暂停自动登录的边沿决策（2026-11-18 A4 降级解）。
 *
 * 纯函数，不碰 Android API：KeeperService 的 NetworkCallback 读出「当前有无 VPN」
 * 这个系统事实喂进来，与用户原本的 auto_reconnect 设定一起算动作。系统事实是
 * 任何 VPN 都走 NetworkCapabilities.TRANSPORT_VPN（状态栏钥匙），不猜具体 App。
 *
 * 规则（用户拍板）：
 * - VPN 出现 → 暂停自动登录（探测展示照常，只是不登录，避免被 TUN 抓走误判
 *   掉线后反复撞校园冻结线）；用户原本就关着 → 全程不动。
 * - VPN 消失 → 立即恢复 + 通知；重复通知（onCapabilitiesChanged 连发）不重复动作。
 */
class VpnGuard {
    enum class Action { NONE, PAUSE, RESUME }

    private var vpnPresent: Boolean? = null // null = 还没建立基线

    fun onVpnChanged(vpnPresent: Boolean, userAutoReconnect: Boolean): Action {
        val previous = this.vpnPresent
        this.vpnPresent = vpnPresent
        if (!userAutoReconnect) return Action.NONE // 用户自己关的，不替他打开
        if (previous == vpnPresent) return Action.NONE // 边沿：只在状态翻转时动作
        return if (vpnPresent) Action.PAUSE else if (previous == true) Action.RESUME else Action.NONE
    }
}
