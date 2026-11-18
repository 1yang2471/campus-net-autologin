package dev.cnal.m0

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.w3c.dom.Element

// M0 实测（docs/m0-findings.md §2）：targetSdk≥28 默认禁明文 HTTP，探针第一
// 轮全线 OFFLINE 的原因就是 "Cleartext HTTP traffic not permitted"。M1 的
// WebView 面板走 http://127.0.0.1:<port>/panel，同一条规则会让它白屏。
//
// 这个测试守的是「白名单真的放行回路、且不扩大到全局」——把 127.0.0.1
// 从 domain-config 里删掉，测试必须红；改成 base-config 全局放行也必须红。
class CleartextPolicyTest {
    private val configFile = File("src/main/res/xml/network_security_config.xml")

    private fun domainsInPermittedConfig(): List<String> {
        val doc = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(configFile)
        doc.documentElement.normalize()
        val configs = doc.getElementsByTagName("domain-config")
        val out = mutableListOf<String>()
        for (i in 0 until configs.length) {
            val cfg = configs.item(i) as Element
            if (cfg.getAttribute("cleartextTrafficPermitted") != "true") continue
            val domains = cfg.getElementsByTagName("domain")
            for (j in 0 until domains.length) {
                out += domains.item(j).textContent.trim()
            }
        }
        return out
    }

    @Test
    fun loopbackPanelIsExplicitlyAllowed() {
        assertTrue("配置文件缺失：${configFile.absolutePath}", configFile.isFile)
        val allowed = domainsInPermittedConfig()
        assertTrue(
            "M1 面板回路 127.0.0.1 必须在 cleartext 放行域里（否则 WebView 白屏）",
            "127.0.0.1" in allowed,
        )
        // 锚点域名保留：M0 的探针不能因为加面板就失效。
        assertTrue("连通性锚点域名必须继续放行", "connect.rom.miui.com" in allowed)
    }

    @Test
    fun baseConfigStillDeniesCleartext() {
        val doc = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(configFile)
        val base = doc.getElementsByTagName("base-config").item(0) as Element
        assertFalse(
            "不得用 base-config 全局放行明文（范围过大，android-plan §8 风险 5）",
            base.getAttribute("cleartextTrafficPermitted") == "true",
        )
    }
}
