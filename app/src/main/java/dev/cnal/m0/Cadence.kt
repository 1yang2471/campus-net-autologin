package dev.cnal.m0

/**
 * 心跳节拍：30s 一拍（A1/A3 的心跳周期）。漏拍 = 相邻两拍间隔超过 1.5 倍周期，
 * 即前台服务被杀 / Doze 冻结 / OEM 阉割的证据。
 *
 * 1.5 倍是给调度抖动的余量：把正常的 ±几秒抖动记成"被杀"会让 A1 结论失真。
 */
object Cadence {
    const val PERIOD_MS = 30_000L

    fun missed(prevMs: Long, nowMs: Long): Int {
        val gap = nowMs - prevMs
        if (gap <= PERIOD_MS * 3 / 2) return 0 // 含 gap <= 0（时钟回拨不算漏拍）
        return ((gap + PERIOD_MS / 2) / PERIOD_MS).toInt() - 1
    }
}
