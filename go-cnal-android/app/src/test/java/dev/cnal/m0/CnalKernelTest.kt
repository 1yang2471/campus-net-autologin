package dev.cnal.m0

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// 门户基址只允许 http(s)：拼进 config.ini 后被 Go 内核拿去发请求，
// 不接受 file/javascript/裸域名，避免把任意 scheme 带进请求链。
class CnalKernelTest {
    @Test
    fun httpAndHttpsAreAccepted() {
        assertTrue(CnalKernel.isValidServer("http://a.stu.edu.cn"))
        assertTrue(CnalKernel.isValidServer("https://portal.example.edu.cn"))
    }

    @Test
    fun otherSchemesAreRejected() {
        assertFalse(CnalKernel.isValidServer("file:///sdcard/x"))
        assertFalse(CnalKernel.isValidServer("javascript:alert(1)"))
        assertFalse(CnalKernel.isValidServer("a.stu.edu.cn"))
        assertFalse(CnalKernel.isValidServer(""))
    }
}
