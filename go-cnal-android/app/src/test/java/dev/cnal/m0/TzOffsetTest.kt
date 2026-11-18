package dev.cnal.m0

import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 时区偏移注入（2026-11-11 M1 首跑实证：gomobile 无 TZ，Go 侧 time.Now 挂
 * UTC，免费时段 23:30-06:30 按 UTC 判定把下午误判成「免认证时段」→ 不自动
 * 登录）。Kotlin 在启动守护前把设备当前偏移分钟注入 Go。
 */
class TzOffsetTest {
    @Test
    fun shanghaiIsPlus480() {
        assertEquals(480, TzOffset.minutesOf(ZoneId.of("Asia/Shanghai")))
    }

    @Test
    fun utcIsZero() {
        assertEquals(0, TzOffset.minutesOf(ZoneId.of("UTC")))
    }

    @Test
    fun newYorkStandardIsMinus300() {
        // 固定 1 月（EST，无夏令时），避免测试机时钟进夏令时区间。
        val est = ZoneId.of("America/New_York")
        val jan = java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone(est)).apply {
            set(2027, 0, 15, 12, 0, 0)
        }
        assertEquals(-300, TzOffset.minutesAt(est, jan.timeInMillis))
    }
}
