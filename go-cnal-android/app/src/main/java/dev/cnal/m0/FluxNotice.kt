package dev.cnal.m0

/**
 * 流量提醒（只读、零副作用）：从 Go 内核 /state 同构的 JSON 里读 flux 档位，
 * 每个阈值档只发一次前台服务通知。内核的 fluxWarner 也保证一次性；这里再
 * 挡一层，是因为 kernelLoop 每 5s 轮询 state，档位不变会反复读到同一 level。
 *
 * 不自动暂停、不断网、不改自动连——只发一条通知。
 *
 * 不用 org.json：android.jar 在 JVM 单测里是 stub（optString 恒返回默认），
 * 解析逻辑就没法在单测里验真实行为。flux 是本项目自己定义的固定结构，
 * 用几个小正则抽字段足够，且能在 JVM 上测（KeeperService 里仍用 JSONObject
 * 读 label/detail，那是 Android 运行时路径）。
 */
object FluxNotice {
    private val reFluxObj = Regex("\"flux\"\\s*:\\s*\\{([^}]*)\\}")
    private fun field(obj: String, key: String): String =
        Regex("\"$key\"\\s*:\\s*\"([^\"]*)\"").find(obj)?.groupValues?.get(1).orEmpty()
    private fun number(obj: String, key: String): Double =
        Regex("\"$key\"\\s*:\\s*([0-9.]+)").find(obj)?.groupValues?.get(1)?.toDoubleOrNull() ?: -1.0
    private fun bool(obj: String, key: String): Boolean =
        Regex("\"$key\"\\s*:\\s*(true|false)").find(obj)?.groupValues?.get(1) == "true"

    private fun fluxObject(stateJson: String): String =
        reFluxObj.find(stateJson)?.groupValues?.get(1).orEmpty()

    /** 读 state JSON 里的 flux.level；没有 flux/解析失败返回空串。 */
    fun level(stateJson: String): String = field(fluxObject(stateJson), "level")

    /** 读 flux 的原始读数（文案用）。 */
    data class Readings(val percent: Int, val used: String, val quota: String, val hasPercent: Boolean)

    fun readings(stateJson: String): Readings {
        val f = fluxObject(stateJson)
        if (f.isEmpty()) return Readings(-1, "", "", false)
        val p = number(f, "percent")
        return Readings(
            if (p < 0) -1 else Math.round(p).toInt(),
            field(f, "used"),
            field(f, "quota"),
            bool(f, "has_percent"),
        )
    }

    fun title(level: String): String = when (level) {
        "critical" -> "校园网流量将用尽"
        else -> "校园网流量提醒"
    }

    /** 短、具体、不喊；算不出百分比就原文照列，不编造数字。 */
    fun body(percent: Int, used: String, quota: String, hasPercent: Boolean): String {
        val pct = if (hasPercent) "，约 $percent%" else ""
        return when {
            used.isNotBlank() && quota.isNotBlank() -> "今日已用 $used / $quota$pct"
            used.isNotBlank() -> "今日已用 $used$pct"
            else -> "已查询到流量信息，请在面板查看"
        }
    }

    /** 一次性闸门：进程生命周期内每个非 ok 档只放行一次，回落也不重发。 */
    class Gate {
        private val fired = mutableSetOf<String>()

        fun shouldNotify(level: String): Boolean {
            if (level.isBlank() || level == "ok" || !fired.add(level)) return false
            return true
        }
    }
}
