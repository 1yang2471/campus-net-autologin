package dev.cnal.m0

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 网络回调 → Go 唤醒的节流过滤（纯逻辑，JVM 可单测）。复盘 §3.2：回调
 * 直调 Mobile.wake 会在弱网抖动时风暴唤醒（capabilities 每秒可变多次），
 * 全吞又违背「事件驱动即时发现」。策略：
 *  - 链路生死事件（onLost/onAvailable）必唤醒——这是掉线/恢复的主信号；
 *  - capabilities/LinkProperties 变化：只在「网络标识实质变化」时唤醒
 *    （validated 翻转、绑定的 NetId 换了），RSSI 级噪声全部吞掉。
 */
class WakeFilterTest {
    // onLost/onAvailable 主信号：必醒。
    @Test
    fun linkLifecycleAlwaysWakes() {
        assertTrue(WakeFilter.shouldWake("onLost"))
        assertTrue(WakeFilter.shouldWake("onAvailable"))
    }

    // capabilities 抖动：RSSI/带宽等噪声不唤醒。
    @Test
    fun capabilityNoiseDoesNotWake() {
        val a = WakeCaps(validated = true, wifi = true, netId = 259)
        assertFalse(WakeFilter.capsChanged(a, a.copy()))
    }

    // validated 翻转（ captive portal 出入、认证状态变化）：唤醒。
    @Test
    fun validatedFlipWakes() {
        val before = WakeCaps(validated = false, wifi = true, netId = 259)
        val after = before.copy(validated = true)
        assertTrue(WakeFilter.capsChanged(before, after))
    }

    // NetId 变化（Wi-Fi 重连换了网络句柄）：唤醒——绑源与 GUA 可能全换。
    @Test
    fun netIdChangeWakes() {
        val before = WakeCaps(validated = true, wifi = true, netId = 259)
        val after = before.copy(netId = 260)
        assertTrue(WakeFilter.capsChanged(before, after))
    }

    // transport 变化（Wi-Fi↔蜂窝）：唤醒。
    @Test
    fun transportChangeWakes() {
        val before = WakeCaps(validated = true, wifi = true, netId = 259)
        val after = before.copy(wifi = false)
        assertTrue(WakeFilter.capsChanged(before, after))
    }

    // 节流兜底：即便判定要醒，距上次唤醒不足 minGapMs 也吞（防风暴）；
    // 从未唤醒过（lastAt=0）不节流——第一次事件必须醒。
    @Test
    fun minGapThrottlesStorm() {
        val now = 10_000L
        assertFalse(WakeFilter.throttled("onLost", lastAt = 0L, now = now, minGapMs = 2_000L))
        assertTrue(WakeFilter.throttled("onLost", lastAt = 9_000L, now = now, minGapMs = 2_000L))
        assertFalse(WakeFilter.throttled("onLost", lastAt = 7_500L, now = now, minGapMs = 2_000L))
    }
}
