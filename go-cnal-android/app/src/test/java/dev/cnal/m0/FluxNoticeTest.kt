package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 流量提醒（只读、零副作用）：Android 前台服务从 /state 同构的 JSON 里读
 * flux.level，每个阈值档只发一次通知。文案是纯函数，平台调用不进单测。
 */
class FluxNoticeTest {
    @Test
    fun parsesLevelFromStateJson() {
        val json = """{"state":"online","flux":{"used":"3.2G","quota":"4G","percent":80,"has_percent":true,"level":"warn"}}"""
        assertEquals("warn", FluxNotice.level(json))
    }

    @Test
    fun missingFluxIsNoLevel() {
        assertEquals("", FluxNotice.level("""{"state":"online"}"""))
        assertEquals("", FluxNotice.level("not json"))
    }

    @Test
    fun firesOncePerLevel() {
        val gate = FluxNotice.Gate()
        assertTrue(gate.shouldNotify("warn"))
        assertFalse(gate.shouldNotify("warn"))     // 同档重复
        assertTrue(gate.shouldNotify("critical"))  // 升档
        assertFalse(gate.shouldNotify("critical"))
        assertFalse(gate.shouldNotify("warn"))     // 回落也不重发
    }

    @Test
    fun neverNotifiesOnOkOrEmpty() {
        val gate = FluxNotice.Gate()
        assertFalse(gate.shouldNotify(""))
        assertFalse(gate.shouldNotify("ok"))
    }

    @Test
    fun messageIsShortAndConcrete() {
        val title = FluxNotice.title("critical")
        val body = FluxNotice.body(80, "3.2G", "4G", true)
        assertTrue(title.isNotBlank())
        assertTrue(body.contains("80"))
        assertTrue(body.contains("3.2G"))
        assertTrue(body.contains("4G"))
        assertFalse("不得带感叹号", (title + body).contains("!") || (title + body).contains("！"))
        assertTrue("标题须短到通知栏放得下", title.length <= 24)
        assertTrue("正文须短", body.length <= 64)
    }

    @Test
    fun messageWithoutPercentKeepsRaw() {
        val body = FluxNotice.body(-1, "3.2G", "4G", false)
        assertTrue(body.contains("3.2G"))
        assertFalse(body.contains("%"))
    }
}
