package dev.cnal.m0

/**
 * 网络回调 → Go 唤醒的节流过滤（纯逻辑，JVM 可单测）。语义见
 * WakeFilterTest：链路生死必醒，capabilities 只在实质变化时醒，加最小
 * 间隔节流防风暴。KeeperService 注册 NetworkCallback 后每事件过这里。
 */
object WakeFilter {
    /** 最小唤醒间隔（ms）：切网瞬间回调连发，2s 内合并为一次。 */
    const val MIN_GAP_MS = 2_000L

    /** 链路生死事件（Wi-Fi 掉/回）：主信号，必醒（再经节流合并）。 */
    fun shouldWake(reason: String): Boolean =
        reason == "onLost" || reason == "onAvailable"

    /** capabilities 实质变化：validated 翻转 / transport 换 / 网络句柄换。 */
    fun capsChanged(before: WakeCaps, after: WakeCaps): Boolean =
        before.validated != after.validated ||
            before.wifi != after.wifi ||
            before.netId != after.netId

    /** 节流：距上次唤醒不足 minGapMs 时吞掉（lastAt=0 表示从未唤醒过）。 */
    fun throttled(reason: String, lastAt: Long, now: Long, minGapMs: Long): Boolean =
        lastAt != 0L && now - lastAt < minGapMs
}

/** NetworkCapabilities 的可比较快照（只留唤醒判定要的字段）。 */
data class WakeCaps(val validated: Boolean, val wifi: Boolean, val netId: Int)
