package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 首屏适用范围说明（公开发布门禁）：任何用户打开 App 第一眼就该知道
 * 这个工具只适配 ac_portal 族、只在参考校验证过。不是检测——检测要触网、
 * 且异校探测本身是另一个功能；这里是一句诚实的话，零副作用。
 */
class SupportNoticeTest {
    @Test
    fun `说明点齐协议族和验证范围`() {
        val text = SupportNotice.text()
        assertTrue(text.contains("ac_portal"))
        assertTrue(text.contains("验证"))
        assertTrue(text.contains("其它学校"))
    }

    @Test
    fun `未配置门户时另加引导`() {
        val configured = SupportNotice.text(portalConfigured = true)
        val unconfigured = SupportNotice.text(portalConfigured = false)
        assertTrue(unconfigured.contains("先设置门户地址"))
        assertEquals(false, configured.contains("先设置门户地址"))
    }

    @Test
    fun `说明不含夸大词`() {
        val text = SupportNotice.text()
        for (word in listOf("保证", "完美", "万能", "所有学校")) {
            assertEquals(false, text.contains(word))
        }
    }
}
