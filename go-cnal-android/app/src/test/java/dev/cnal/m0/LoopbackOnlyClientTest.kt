package dev.cnal.m0

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// WebView 的 host 白名单：只允许 127.0.0.1:<port> 这一个回路端点。
// 拦住 localhost 别名、环回别名、私网/保留地址、公网与非 http(s) scheme。
class LoopbackOnlyClientTest {
    private fun allow(s: String) = LoopbackOnlyClient.isLoopbackPanel(s)

    @Test
    fun loopbackPanelIsAllowed() {
        assertTrue(allow("http://127.0.0.1:38471/panel?token=abc"))
        assertTrue(allow("http://127.0.0.1:38471/state"))
    }

    @Test
    fun otherHostsAreRejected() {
        assertFalse(allow("http://localhost:8080/panel"))
        assertFalse(allow("http://[::1]:8080/panel"))
        assertFalse(allow("http://10.0.0.1/panel"))
        assertFalse(allow("http://192.168.1.1/panel"))
        assertFalse(allow("http://169.254.169.254/latest/meta-data"))
        assertFalse(allow("https://example.com/panel"))
    }

    @Test
    fun badSchemeOrMissingPortIsRejected() {
        assertFalse(allow("file:///sdcard/panel.html"))
        assertFalse(allow("javascript:alert(1)"))
        assertFalse(allow("http://127.0.0.1/panel"))
    }
}
