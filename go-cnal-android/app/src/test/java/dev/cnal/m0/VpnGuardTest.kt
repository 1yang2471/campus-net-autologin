package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 网络不适合校园认证时的暂停守卫（2026-11-18：VPN → 扩到蜂窝直连/热点）。
 *
 * 纯函数：输入「默认网络是否 VPN」「是否只剩蜂窝」+「用户原本的 auto_reconnect
 * 设定」，输出边沿动作（暂停/恢复/无动作）。KeeperService 从 NetworkCallback
 * 取系统事实喂进来，动作落到既有 /config 热改缝。判据（用户拍板）：
 * - 有 VPN（任何 VPN，状态栏钥匙）→ 暂停自动登录，探测展示照常；
 * - 默认网络只剩蜂窝（开热点 AP 时手机通常掉 Wi-Fi）→ 暂停，避免用蜂窝撞门户；
 * - 两者都消失 → 立即恢复 + 通知；
 * - 用户原本就关着 auto_reconnect → 全程不动（恢复时不能替用户打开）。
 */
class VpnGuardTest {
    @Test
    fun `VPN 出现时暂停，消失时恢复`() {
        val g = VpnGuard()
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = false, userAutoReconnect = true),
        )
        assertEquals(
            VpnGuard.Action.PAUSE,
            g.onNetworkChanged(vpnPresent = true, cellularOnly = false, userAutoReconnect = true),
        )
        // 重复通知（onCapabilitiesChanged 会反复来）不得重复暂停
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = true, cellularOnly = false, userAutoReconnect = true),
        )
        assertEquals(
            VpnGuard.Action.RESUME,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = false, userAutoReconnect = true),
        )
        assertEquals("vpn", g.pauseReason())
    }

    @Test
    fun `蜂窝直连时暂停，连上 Wi-Fi 恢复`() {
        val g = VpnGuard()
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = false, userAutoReconnect = true),
        )
        assertEquals(
            VpnGuard.Action.PAUSE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = true, userAutoReconnect = true),
        )
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = true, userAutoReconnect = true),
        )
        assertEquals(
            VpnGuard.Action.RESUME,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = false, userAutoReconnect = true),
        )
        assertEquals("cellular", g.pauseReason())
    }

    @Test
    fun `VPN 与蜂窝同时存在只暂停一次，VPN 关掉仍保持暂停`() {
        val g = VpnGuard()
        assertEquals(
            VpnGuard.Action.PAUSE,
            g.onNetworkChanged(vpnPresent = true, cellularOnly = true, userAutoReconnect = true),
        )
        // VPN 关掉，但仍是蜂窝直连：不能恢复（否则会用蜂窝撞门户）
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = true, userAutoReconnect = true),
        )
        assertEquals(
            VpnGuard.Action.RESUME,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = false, userAutoReconnect = true),
        )
    }

    @Test
    fun `用户原本关着自动重连则全程不动`() {
        val g = VpnGuard()
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = true, cellularOnly = false, userAutoReconnect = false),
        )
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = false, userAutoReconnect = false),
        )
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = false, cellularOnly = true, userAutoReconnect = false),
        )
    }

    @Test
    fun `服务重启期间 VPN 状态从未知稳定下来不误报动作`() {
        // 首调建立基线：VPN 开着 + 用户设定开 → 应暂停（用户可能需要先关）
        val g = VpnGuard()
        assertEquals(
            VpnGuard.Action.PAUSE,
            g.onNetworkChanged(vpnPresent = true, cellularOnly = false, userAutoReconnect = true),
        )
        // 再调同样状态 → 无动作
        assertEquals(
            VpnGuard.Action.NONE,
            g.onNetworkChanged(vpnPresent = true, cellularOnly = false, userAutoReconnect = true),
        )
    }
}