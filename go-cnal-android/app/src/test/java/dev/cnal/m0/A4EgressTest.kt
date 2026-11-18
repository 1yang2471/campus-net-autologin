package dev.cnal.m0

import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * A4 绑网核对的读数解析（`android-a1-a5-testplan.md` §4）。
 *
 * 门户的 `opr=ipv4or6online` 应答应答是 **单引号字典，不是 JSON**
 * （真机实测：`{'success':true, 'ip':'172.29.153.170', 'isOnline':true}`），
 * 所以不能用 JSONObject 解——这条测试就是钉住"别换回 JSON 解析"。
 */
class A4EgressTest {
    @Test
    fun parsesSingleQuotedPortalReply() {
        val body = "{'success':true, 'ip':'172.29.153.170', 'isOnline':true}"
        assertEquals("172.29.153.170", A4Egress.extractIp(body))
    }

    @Test
    fun parsesWithSpacesAndOtherKeys() {
        val body = "{ \"success\" : true , 'ip' : '2001:250:3003:aa29::1' , 'isOnline': true }"
        assertEquals("2001:250:3003:aa29::1", A4Egress.extractIp(body))
    }

    @Test
    fun returnsNullWhenNoIp() {
        assertNull(A4Egress.extractIp("{'success':false,'msg':'not taken'}"))
        assertNull(A4Egress.extractIp(""))
    }

    /** A4 日志行必须单行、字段定序，脚本才能用 `A4 .* verdict=PASS` 统计通过率。 */
    @Test
    fun a4LogLineIsSingleLineAndOrdered() {
        val t = 1_700_000_000_000L
        val line = ProbeLog.a4(t, "172.29.153.170", "172.29.153.170", "172.29.153.170",
            "Wi-Fi", "PASS", ZoneId.of("Asia/Shanghai"))
        assertTrue("A4 行不得含换行", !line.contains("\n"))
        assertTrue("A4 行必须带可抓的前缀与判定", line.contains(" A4 ") && line.contains("verdict=PASS"))
        // 字段顺序：wifi → shell → go → verdict → iface
        val order = listOf("wifi=", "shell=", "go=", "verdict=", "iface=")
            .map { line.indexOf(it) }
        assertEquals("字段顺序应固定（否则脚本切分漂移）", order.sorted(), order)
        // 时间戳按传入时区渲染（整点那一拍也必须带全字段——ProbeLog 的 FMT 注释说过
        // OffsetDateTime.toString() 会省略为零的秒/毫秒，所以这里比对"解析得回去"）。
        val ts = line.substringBefore(" A4 ")
        assertEquals(
            "时间戳应是可解析的 RFC3339（按传入时区）",
            java.time.Instant.ofEpochMilli(t).atZone(ZoneId.of("Asia/Shanghai"))
                .format(java.time.format.DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSSXXX")),
            ts,
        )
    }
}