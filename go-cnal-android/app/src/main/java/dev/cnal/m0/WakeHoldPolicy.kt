package dev.cnal.m0

/**
 * 唤醒锁上限策略（纯逻辑，JVM 可单测）。复盘 §3.3：`acquire()` 无超时，
 * 「屏灭 + 被拒」可无限期持锁污染 A3 电耗归因。本策略在 PowerPolicy 的
 * 单拍决策之上叠加**连续持有时长**维度：单拍该持 ≠ 可以一直持。
 *
 * 用法（kernelLoop 每 5s 一轮）：把上一轮决策（hold）与累计持有毫秒
 * （holdMs）喂进来，拿回本轮决策。上限语义交给 acquire(timeout) 兜底，
 * 这里到点转 RELEASE 让调用方主动放锁并归零计时。
 */
object WakeHoldPolicy {
    const val DEFAULT_LIMIT_MS = 120_000L // 2 分钟：认证 RADIUS 后端 3–6s，远超够用

    fun decide(hold: Boolean, holdMs: Long, limitMs: Long, pollMs: Long,
               base: PowerPolicy.WakeDecision): String {
        if (base == PowerPolicy.WakeDecision.RELEASE) return "RELEASE"
        // 单拍判 HOLD：已连续持满上限 → 放（acquire(timeout) 之前的主动防线）。
        return if (hold && holdMs >= limitMs) "RELEASE" else "HOLD"
    }
}
