package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 校园 DNS 自动注入的判据测试（2026-11-19 真机摩擦：新装机要手抄 campus_dns）。
 * 守三条行为：挑 v4 优先、不覆盖已有值、缺键时落到正确位置。
 */
class CampusDnsTest {
    @Test
    fun `pick 优先 IPv4`() {
        assertEquals("10.129.17.10", CampusDns.pick(listOf("2001:db8::53", "10.129.17.10")))
        assertEquals("10.1.1.1", CampusDns.pick(listOf("10.1.1.1", "10.1.1.2")))
    }

    @Test
    fun `pick 只有 v6 时取第一条，空列表返回空`() {
        assertEquals("2001:db8::53", CampusDns.pick(listOf("2001:db8::53")))
        assertEquals("", CampusDns.pick(emptyList()))
        assertEquals("", CampusDns.pick(listOf("  ")))
    }

    @Test
    fun `ensure 不覆盖已有生效值`() {
        val lines = listOf("[keeper]", "campus_dns = 10.9.9.9", "server = https://a.example")
        assertEquals(lines, CampusDns.ensure(lines, "10.1.1.1"))
    }

    @Test
    fun `ensure 填充空值的生效行`() {
        val lines = listOf("[keeper]", "campus_dns =", "server = https://a.example")
        val out = CampusDns.ensure(lines, "10.1.1.1")
        assertEquals(listOf("[keeper]", "campus_dns = 10.1.1.1", "server = https://a.example"), out)
    }

    @Test
    fun `ensure 替换注释占位行`() {
        val lines = listOf(
            "[keeper]",
            "server = https://a.example",
            "; campus_dns = 10.x.x.x   ; 已知校园 DNS 时填上",
            "login_path = /ac_portal/login.php",
        )
        val out = CampusDns.ensure(lines, "10.1.1.1")
        assertEquals(
            listOf(
                "[keeper]",
                "server = https://a.example",
                "campus_dns = 10.1.1.1",
                "login_path = /ac_portal/login.php",
            ),
            out,
        )
    }

    @Test
    fun `ensure 无占位行时插在 keeper 段首，无 keeper 时追加末尾`() {
        val withKeeper = listOf("; hi", "[keeper]", "server = https://a.example")
        assertEquals(
            listOf("; hi", "[keeper]", "campus_dns = 10.1.1.1", "server = https://a.example"),
            CampusDns.ensure(withKeeper, "10.1.1.1"),
        )
        val noKeeper = listOf("server = https://a.example")
        assertEquals(
            listOf("server = https://a.example", "campus_dns = 10.1.1.1"),
            CampusDns.ensure(noKeeper, "10.1.1.1"),
        )
    }

    @Test
    fun `ensure 无 DNS 时原样返回`() {
        val lines = listOf("[keeper]", "server = https://a.example")
        assertEquals(lines, CampusDns.ensure(lines, ""))
    }
}
