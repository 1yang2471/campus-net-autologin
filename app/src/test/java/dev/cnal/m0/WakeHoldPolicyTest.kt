package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 唤醒锁上限策略（纯逻辑，JVM 可单测）。复盘 §3.3：acquire() 无超时，
 * 「屏灭 + 被拒」可无限期持锁污染 A3 电耗归因。修复 = acquire(timeout)
 * 上限 + 到点自动放锁；本策略只算「该不该续」，不管 Android API。
 */
class WakeHoldPolicyTest {
    private companion object {
        const val HOLD_MS = 120_000L // 2 分钟上限（认证 RADIUS 后端 3–6s，远超够用）
        const val POLL_MS = 5_000L
    }

    @Test
    fun capsContinuousHoldAtLimit() {
        // 被拒态屏灭连挂 60 个轮询周期（300s）：单次决策永远 HOLD，但连续
        // 持有时长到上限必须断开重评——断开后重新计时（下轮可再 HOLD），
        // 但 held 绝不超过上限。这正是「无限期持锁」的病灶修复判据。
        var held = 0L
        var decision = "HOLD"
        var releases = 0
        for (i in 0 until 60) {
            decision = WakeHoldPolicy.decide(
                hold = decision == "HOLD", holdMs = held,
                limitMs = HOLD_MS, pollMs = POLL_MS,
                base = PowerPolicy.decide("被拒", screenOn = false, portalReachable = true))
            held = if (decision == "HOLD") held + POLL_MS else 0L
            if (decision == "RELEASE") releases++
            assert(held <= HOLD_MS) { "held 超上限: $held @cycle $i" }
        }
        assertEquals("60 周期（300s）应至少断开 2 次（上限 120s）", 2, releases)
    }

    @Test
    fun screenOnResetsTheClock() {
        // 持锁 90s 后亮屏（RELEASE 归零），再屏灭被拒：重新计时，不该被旧账判死。
        var held = 90_000L
        var decision = "HOLD"
        decision = WakeHoldPolicy.decide(hold = true, holdMs = held,
            limitMs = HOLD_MS, pollMs = POLL_MS,
            base = PowerPolicy.WakeDecision.RELEASE) // 亮屏 → RELEASE
        assertEquals("RELEASE", decision)
        held = if (decision == "HOLD") held + POLL_MS else 0L
        assertEquals(0L, held)
        decision = WakeHoldPolicy.decide(hold = false, holdMs = held,
            limitMs = HOLD_MS, pollMs = POLL_MS,
            base = PowerPolicy.decide("被拒", screenOn = false, portalReachable = true))
        assertEquals("HOLD", decision)
    }

    @Test
    fun shortAuthWindowUnaffected() {
        // 正常认证窗口（30s 内完成）：全程 HOLD，不受上限影响。
        var held = 0L
        var decision = "HOLD"
        for (i in 0 until 6) {
            decision = WakeHoldPolicy.decide(
                hold = decision == "HOLD", holdMs = held,
                limitMs = HOLD_MS, pollMs = POLL_MS,
                base = PowerPolicy.decide("离线", screenOn = false, portalReachable = true))
            held = if (decision == "HOLD") held + POLL_MS else 0L
        }
        assertEquals("HOLD", decision)
        assertEquals(30_000L, held)
    }

    @Test
    fun releaseAlsoCountsWhenNotHeld() {
        // 根本没持锁时（base=RELEASE，如屏幕亮着）永远 RELEASE，计时归零。
        val decision = WakeHoldPolicy.decide(hold = false, holdMs = 0L,
            limitMs = HOLD_MS, pollMs = POLL_MS,
            base = PowerPolicy.WakeDecision.RELEASE)
        assertEquals("RELEASE", decision)
    }
}
