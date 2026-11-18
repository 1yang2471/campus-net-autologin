package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.ZoneOffset

// M0 的纯逻辑单测（JVM，不上模拟器）：探针判定 / 漏拍计数 / 选网 / 日志行。
// 这些是能脱离 Android 框架钉住的部分；前台服务存活与绑网实效只能靠真机
// （模拟器不受 Doze/OEM 阉割，按 docs/android-a1-a5-testplan.md 不参与 A1–A5
// 判定）。

class ProbeLogicTest {
    // 行为：204 与无劫持标记的 200 → 在线（锚点探测成功的两种正常形态）。
    @Test
    fun plainSuccessIsOnline() {
        assertEquals(ProbeVerdict.ONLINE, ProbeLogic.verdict(204, ""))
        assertEquals(ProbeVerdict.ONLINE, ProbeLogic.verdict(200, "<html>ok</html>"))
    }

    // 行为：30x 一律判劫持——门户重定向不是"真实互联网"。
    @Test
    fun redirectIsHijacked() {
        for (code in listOf(301, 302, 307, 308)) {
            assertEquals("code=$code", ProbeVerdict.HIJACKED, ProbeLogic.verdict(code, ""))
        }
    }

    // 行为：200 但正文带门户指纹（JS 挑战页）→ 劫持，不是在线。
    @Test
    fun portalFingerprintInBodyIsHijacked() {
        assertEquals(ProbeVerdict.HIJACKED, ProbeLogic.verdict(200, "<a href='/ac_portal/login.php'>"))
        assertEquals(ProbeVerdict.HIJACKED, ProbeLogic.verdict(200, "Portal 认证"))
    }

    // 行为：其它状态码（含 5xx/4xx）按离线算，不猜成在线。
    @Test
    fun otherCodesAreOffline() {
        assertEquals(ProbeVerdict.OFFLINE, ProbeLogic.verdict(500, ""))
        assertEquals(ProbeVerdict.OFFLINE, ProbeLogic.verdict(404, ""))
        assertEquals(ProbeVerdict.OFFLINE, ProbeLogic.verdict(0, "")) // 传输出错
    }
}

class CadenceTest {
    @Test
    fun onTimeBeatIsNotMissed() {
        assertEquals(0, Cadence.missed(1_000L, 1_000L + Cadence.PERIOD_MS))
    }

    // 边界：1.5 倍周期内不算漏拍（调度抖动不该被记成被杀）。
    @Test
    fun oneAndHalfPeriodIsStillNotMissed() {
        assertEquals(0, Cadence.missed(0L, Cadence.PERIOD_MS * 3 / 2))
    }

    @Test
    fun longerGapsCountMissedBeats() {
        assertEquals(1, Cadence.missed(0L, Cadence.PERIOD_MS * 2))
        assertEquals(2, Cadence.missed(0L, Cadence.PERIOD_MS * 3))
        assertEquals(9, Cadence.missed(0L, Cadence.PERIOD_MS * 10))
    }

    @Test
    fun clockGoingBackwardsIsNotAMiss() {
        assertEquals(0, Cadence.missed(5_000L, 1_000L))
    }
}

class NetPickTest {
    private fun cand(wifi: Boolean, vpn: Boolean, validated: Boolean) =
        NetCandidate(isWifi = wifi, hasVpn = vpn, isValidated = validated)

    // 行为：选 Wi-Fi 且不含 VPN——这是 A4 的前提（绑错网等于探测全不可信）。
    @Test
    fun prefersWifiWithoutVpn() {
        assertEquals(1, NetPick.pickIndex(listOf(cand(false, false, true), cand(true, false, true))))
    }

    @Test
    fun prefersValidatedAmongWifi() {
        assertEquals(1, NetPick.pickIndex(listOf(cand(true, false, false), cand(true, false, true))))
    }

    // 行为：只有带 VPN 的 Wi-Fi / 没有 Wi-Fi → 交白卷（-1），不退回蜂窝瞎绑。
    @Test
    fun noEligibleNetworkIsNegativeOne() {
        assertEquals(-1, NetPick.pickIndex(listOf(cand(true, true, true), cand(false, false, true))))
        assertEquals(-1, NetPick.pickIndex(emptyList()))
    }
}

class ProbeLogTest {
    // 行为：日志行固定形态——时间戳带毫秒与偏移，字段可 grep（M0 的漏拍/时延
    // 全靠它算，形态漂了 A1/A3 就没法算）。
    @Test
    fun lineFormatIsStable() {
        val s = ProbeLog.line(1_700_000_000_123L, 204, 37, ProbeVerdict.ONLINE, ZoneOffset.UTC)
        assertEquals("2023-11-14T22:13:20.123Z code=204 ms=37 ONLINE", s)
    }

    @Test
    fun offlineLineCarriesZeroCode() {
        val s = ProbeLog.line(0L, 0, 5000, ProbeVerdict.OFFLINE, ZoneOffset.UTC)
        assertEquals("1970-01-01T00:00:00.000Z code=0 ms=5000 OFFLINE", s)
    }

    // 行为：失败原因必须落进日志。只记 OFFLINE 的话，"没绑上网"和"明文 HTTP 被
    // 系统拦了"在日志里一模一样，A4 就无从判断（2026-11-09 本机实证：锚点走
    // 明文 http，targetSdk 35 默认禁 cleartext，探针全灭且无声）。
    @Test
    fun failureReasonIsAppended() {
        val s = ProbeLog.line(0L, 0, 3, ProbeVerdict.OFFLINE, ZoneOffset.UTC,
            "Cleartext HTTP traffic not permitted")
        assertEquals(
            "1970-01-01T00:00:00.000Z code=0 ms=3 OFFLINE err=Cleartext HTTP traffic not permitted",
            s,
        )
    }
}
