package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * VPN 暂停自动登录（2026-11-18 A4 判定降级解）。
 *
 * 纯函数：输入「当前有无 VPN」+「用户原本的 auto_reconnect 设定」，输出边沿动作
 * （暂停/恢复/无动作）。KeeperService 从 NetworkCallback 取系统事实喂进来，
 * 动作落到既有 /config 热改缝。判据（用户拍板）：
 * - 有 VPN（任何 VPN，状态栏钥匙）→ 暂停自动登录，探测展示照常
 * - VPN 消失 → 立即恢复 + 通知
 * - 用户原本就关着 auto_reconnect → 全程不动（恢复时不能替用户打开）
 */
class VpnGuardTest {
    @Test
    fun `VPN 出现时暂停，消失时恢复`() {
        val g = VpnGuard()
        assertEquals(VpnGuard.Action.NONE, g.onVpnChanged(vpnPresent = false, userAutoReconnect = true))
        assertEquals(VpnGuard.Action.PAUSE, g.onVpnChanged(vpnPresent = true, userAutoReconnect = true))
        // 重复通知（onCapabilitiesChanged 会反复来）不得重复暂停
        assertEquals(VpnGuard.Action.NONE, g.onVpnChanged(vpnPresent = true, userAutoReconnect = true))
        assertEquals(VpnGuard.Action.RESUME, g.onVpnChanged(vpnPresent = false, userAutoReconnect = true))
    }

    @Test
    fun `用户原本关着自动重连则全程不动`() {
        val g = VpnGuard()
        assertEquals(VpnGuard.Action.NONE, g.onVpnChanged(vpnPresent = true, userAutoReconnect = false))
        assertEquals(VpnGuard.Action.NONE, g.onVpnChanged(vpnPresent = false, userAutoReconnect = false))
    }

    @Test
    fun `服务重启期间 VPN 状态从未知稳定下来不误报动作`() {
        // 首调建立基线：VPN 开着 + 用户设定开 → 应暂停（用户可能需要先关）
        val g = VpnGuard()
        assertEquals(VpnGuard.Action.PAUSE, g.onVpnChanged(vpnPresent = true, userAutoReconnect = true))
        // 再调同样状态 → 无动作
        assertEquals(VpnGuard.Action.NONE, g.onVpnChanged(vpnPresent = true, userAutoReconnect = true))
    }
}
