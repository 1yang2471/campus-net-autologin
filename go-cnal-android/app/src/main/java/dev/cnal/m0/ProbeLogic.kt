package dev.cnal.m0

import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * 探针判定：与 go-cnal 的 L1 锚点同语义（`internal/app/compose_daemon.go`
 * `ProbeOnline`）——30x 与"200 但正文带门户指纹"都是劫持，不是真实互联网。
 * 判定错了 M0 的在线率就没有意义，所以这块单独拎出来做纯函数单测。
 */
enum class ProbeVerdict { ONLINE, OFFLINE, HIJACKED }

object ProbeLogic {
    // M0 默认指纹表（参考校 + 常见门户）。异校词表在 go-cnal 是配置面；测量壳
    // 不引入第二套配置，只保证"劫持页不被算成在线"。
    private val HIJACK_MARKS = listOf("ac_portal", "pwdlogin", "portal", "认证")

    fun verdict(httpCode: Int, body: String): ProbeVerdict = when {
        httpCode == 301 || httpCode == 302 || httpCode == 307 || httpCode == 308 ->
            ProbeVerdict.HIJACKED
        httpCode == 200 && hasHijackMark(body) -> ProbeVerdict.HIJACKED
        httpCode == 200 || httpCode == 204 -> ProbeVerdict.ONLINE
        else -> ProbeVerdict.OFFLINE // 含 code=0（传输出错）
    }

    private fun hasHijackMark(body: String): Boolean =
        HIJACK_MARKS.any { body.contains(it, ignoreCase = true) }
}

/**
 * 探针日志行。M0 的漏拍次数与恢复时延全靠这个固定形态算——形态一漂，
 * A1/A3 就没法从日志里算出来。
 *
 * 必须用显式 pattern：`OffsetDateTime.toString()` 会**省略**为零的秒/毫秒
 * （整点那一拍写成 `1970-01-01T00:00Z`），列宽随拍变化，按位切分日志的
 * 做法会在整点崩掉（本测试第一版就是这么红的）。
 */
object ProbeLog {
    // XXX（大写）才把零偏移写成 Z（RFC3339 形态，与 go-cnal 日志口径同形）；
    // 小写 xxx 会写 +00:00，同一台机器换个时区就两种写法。
    private val FMT: DateTimeFormatter =
        DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSSXXX", Locale.ROOT)

    /**
     * 前四个字段定宽（时间戳 code ms VERDICT），失败原因作为 `err=…` 尾巴。
     * 钉住前半段是为了按位/按 token 切日志的算法不受原因文本影响。
     */
    fun line(
        tsMillis: Long,
        code: Int,
        ms: Long,
        verdict: ProbeVerdict,
        zone: ZoneId,
        reason: String = "",
    ): String {
        val ts = Instant.ofEpochMilli(tsMillis).atZone(zone).format(FMT)
        val head = "$ts code=$code ms=$ms $verdict"
        return if (reason.isBlank()) head else "$head err=$reason"
    }

    /**
     * A4 绑网核对行（`android-a1-a5-testplan.md` §4）。单行、字段定序，便于脚本
     * 用 `A4 .* verdict=PASS` 抓通过率（A4 的通过线是「出口正确率 100%」）。
     *
     *   <ts> A4 wifi=<地址> shell=<门户看到的> go=<门户看到的> verdict=PASS|FAIL iface=<绑定描述>
     *
     * wifi 是本机 Wi-Fi 地址，shell 是 Kotlin socket 的出口、go 是 Go native socket
     * 的出口（M1 登录走后者）。三者相等才 PASS。
     */
    fun a4(
        tsMillis: Long,
        wifi: String,
        shell: String,
        go: String,
        iface: String,
        verdict: String,
        zone: ZoneId,
    ): String {
        val ts = Instant.ofEpochMilli(tsMillis).atZone(zone).format(FMT)
        return "$ts A4 wifi=$wifi shell=$shell go=$go verdict=$verdict iface=$iface"
    }
}
