package dev.cnal.m0

import java.time.ZoneId
import java.time.ZoneOffset

/**
 * 设备时区偏移（分钟）计算：注入给 Go 内核做免费时段的本地判定。
 * 纯函数，JVM 单测可直接验真实时区规则（含历史夏令时表）。
 */
object TzOffset {
    /** 当前时刻的偏移分钟（东八区 = +480）。 */
    fun minutesOf(zone: ZoneId): Int = minutesAt(zone, System.currentTimeMillis())

    /** 指定时刻的偏移分钟（用 ZoneRules，夏令时/历史时区都算得对）。 */
    fun minutesAt(zone: ZoneId, epochMillis: Long): Int {
        val offset: ZoneOffset = zone.rules.getOffset(java.time.Instant.ofEpochMilli(epochMillis))
        return offset.totalSeconds / 60
    }
}
