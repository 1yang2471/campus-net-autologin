package dev.cnal.m0

import java.io.DataOutputStream
import java.net.HttpURLConnection
import java.net.URL

/**
 * A4 绑网核对的读数组件（只读、无凭据）。
 *
 * A4 的通过线是「门户侧看到的出口 IP = 校园网出口」（`android-a1-a5-testplan.md` §4），
 * 而 M0 探针只记状态码，看不到出口 IP。这里打门户的 `opr=ipv4or6online`——它按请求
 * 源地址回报客户端 IP，**构造上不带凭据**。
 *
 * 两条路各测一次，因为 A4 真正要回答的是第二个问题：
 *  1. [portalSeenIpViaShell] Kotlin HttpURLConnection（Java 层 socket，官方承诺受
 *     `bindProcessToNetwork` 影响）；
 *  2. [portalSeenIpViaGo] Go native socket + LocalAddr 绑源（**官方只承诺 Java 层**，
 *     M1 登录走的就是这条——它才是必须证的那条）。
 * 两条读数一致且都等于 Wi-Fi 地址 = 通过。
 */
object A4Egress {
    /** 门户应答形如 `{'success':true, 'ip':'172.29.153.170', 'isOnline':true}`（单引号，非 JSON）。 */
    private val reIp = Regex("'ip'\\s*:\\s*'([^']*)'")

    fun extractIp(body: String): String? = reIp.find(body)?.groupValues?.get(1)

    /** Kotlin 侧（Java socket）问门户：返回门户看到的客户端地址。 */
    fun portalSeenIpViaShell(loginUrl: String, timeoutMs: Int = 8000): String {
        val conn = URL(loginUrl).openConnection() as HttpURLConnection
        return try {
            conn.requestMethod = "POST"
            conn.doOutput = true
            conn.connectTimeout = timeoutMs
            conn.readTimeout = timeoutMs
            conn.setRequestProperty("Content-Type", "application/x-www-form-urlencoded")
            conn.setRequestProperty("User-Agent", "cnal-m0-a4")
            DataOutputStream(conn.outputStream).use {
                it.writeBytes("opr=ipv4or6online")
            }
            val body = (if (conn.responseCode in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader()?.use { r -> r.readText() }.orEmpty()
            extractIp(body) ?: throw IllegalStateException("门户未回报 ip：${body.take(160)}")
        } finally {
            conn.disconnect()
        }
    }

    /**
     * Go 侧（native socket + 绑源）：同一条无凭据请求，走 M1 登录那条路。
     * insecure = ssl_verify=false（安卓系统信任库不含校园 CA，实测）；跳过 CA 链
     * 但门户主机名仍强制校验（与桌面同一套语义）。
     */
    fun portalSeenIpViaGo(loginUrl: String, insecure: Boolean): String {
        val body = CnalKernel.egress(loginUrl, insecure)
        return extractIp(body) ?: throw IllegalStateException("门户未回报 ip：${body.take(160)}")
    }
}