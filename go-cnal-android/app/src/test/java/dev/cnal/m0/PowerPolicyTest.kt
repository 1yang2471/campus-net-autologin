package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 唤醒锁策略（纯逻辑）。A1 实证：屏幕熄灭后 MIUI Doze 冻住 m0-probe 线程
 * （23 拍 vs 正常 ~65 拍，19 分钟零拍），进程/服务/线程都在，只是不执行。
 * PARTIAL_WAKE_LOCK 能让 CPU 在屏灭后继续跑 tick，但不能一直持着——
 * 已认证后还持着就是白白耗电。这里决定「持 / 不持 / 放并提醒加白名单」。
 */
class PowerPolicyTest {
    @Test
    fun releasesWhenOnline() {
        assertEquals(PowerPolicy.WakeDecision.RELEASE,
            PowerPolicy.decide("在线", screenOn = true, portalReachable = true))
        assertEquals(PowerPolicy.WakeDecision.RELEASE,
            PowerPolicy.decide("在线", screenOn = false, portalReachable = true))
    }

    @Test
    fun releasesInFreeWindow() {
        // 免认证时段没有登录动作，持锁无意义。
        assertEquals(PowerPolicy.WakeDecision.RELEASE,
            PowerPolicy.decide("免认证时段", screenOn = false, portalReachable = true))
    }

    @Test
    fun releasesWhenPortalUnreachable() {
        // 门户不可达时持锁也发不出登录请求，等它恢复即可。
        assertEquals(PowerPolicy.WakeDecision.RELEASE,
            PowerPolicy.decide("离线", screenOn = false, portalReachable = false))
    }

    @Test
    fun holdsWhileAuthPendingOffScreen() {
        // 认证进行中 + 屏灭 + 门户可达 → 持锁，别让 Doze 冻住登录。
        assertEquals(PowerPolicy.WakeDecision.HOLD,
            PowerPolicy.decide("离线", screenOn = false, portalReachable = true))
        assertEquals(PowerPolicy.WakeDecision.HOLD,
            PowerPolicy.decide("启动中", screenOn = false, portalReachable = true))
    }

    @Test
    fun screenOnDoesNotNeedLock() {
        // 屏幕亮着时 Doze 不生效，持锁是浪费。
        assertEquals(PowerPolicy.WakeDecision.RELEASE,
            PowerPolicy.decide("离线", screenOn = true, portalReachable = true))
    }

    @Test
    fun asksForWhitelistWhenAuthPendingOffScreen() {
        // 认证进行中 + 屏灭 + 门户可达 + 没加白名单 → 提醒用户去加。
        assertTrue(PowerPolicy.shouldAskForWhitelist(
            "离线", screenOn = false, portalReachable = true, whitelisted = false))
        // 加了白名单就别再弹（弹一次够了，反复弹是骚扰）。
        assertFalse(PowerPolicy.shouldAskForWhitelist(
            "离线", screenOn = false, portalReachable = true, whitelisted = true))
    }

    @Test
    fun neverAsksWhenOnlineOrPortalDown() {
        assertFalse(PowerPolicy.shouldAskForWhitelist(
            "在线", screenOn = false, portalReachable = true, whitelisted = false))
        assertFalse(PowerPolicy.shouldAskForWhitelist(
            "离线", screenOn = false, portalReachable = false, whitelisted = false))
    }

    @Test
    fun whitelistIntentTargetsBatterySettings() {
        // 必须是能真正跳到「应用电池优化」设置页的动作（不是随便一个 action）。
        assertTrue(PowerPolicy.whitelistAction().contains("IGNORE_BATTERY_OPTIMIZATION") ||
            PowerPolicy.whitelistAction().contains("BATTERY_OPTIMIZATION"))
    }
}