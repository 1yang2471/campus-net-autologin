package dev.cnal.m0

import android.net.Uri
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL

/**
 * 一次探针（阻塞 IO，调用方必须在后台线程）。与 go-cnal 的 L1 锚点同语义：
 * 不跟随重定向（要看见 302 才能判劫持），只读正文前 4KB（判指纹够用）。
 *
 * 两道闸在发请求之前：只允许 http/https；锚点必须是公网主机（AnchorPolicy）。
 * 闸不过就报 OFFLINE 并写明原因——闸是安全边界，不是提示。
 *
 * 失败一律带 [Result.reason]：只写 OFFLINE 的话，"没绑上网"和"明文 HTTP 被系统
 * 拦了"在日志里一模一样（2026-11-09 本机实证，见 docs/m0-findings.md）。
 */
object ProbeRunner {
    const val BODY_LIMIT = 4096
    private const val REASON_LIMIT = 160

    data class Result(
        val code: Int,
        val ms: Long,
        val verdict: ProbeVerdict,
        val reason: String = "",
    )

    fun once(rawUrl: String, timeoutMs: Int): Result {
        val uri = Uri.parse(rawUrl)
        if (uri.scheme != "http" && uri.scheme != "https") {
            return Result(0, 0, ProbeVerdict.OFFLINE, "闸：非 http/https 锚点（${uri.scheme}）")
        }
        if (!AnchorPolicy.allows(uri.host)) {
            return Result(0, 0, ProbeVerdict.OFFLINE, "闸：锚点非公网主机（${uri.host}）")
        }

        val started = System.currentTimeMillis()
        var conn: HttpURLConnection? = null
        return try {
            val c = URL(rawUrl).openConnection() as HttpURLConnection
            conn = c
            c.instanceFollowRedirects = false
            c.connectTimeout = timeoutMs
            c.readTimeout = timeoutMs
            c.requestMethod = "GET"
            val code = c.responseCode
            val body = readHead(if (code in 200..299) c.inputStream else c.errorStream, BODY_LIMIT)
            Result(code, System.currentTimeMillis() - started, ProbeLogic.verdict(code, body))
        } catch (e: Exception) {
            Result(0, System.currentTimeMillis() - started, ProbeVerdict.OFFLINE, describe(e))
        } finally {
            conn?.disconnect()
        }
    }

    /** 异常摘要：类名 + message（系统拦明文 HTTP 时 message 就是那句原话）。 */
    private fun describe(e: Exception): String {
        val msg = e.message.orEmpty().replace('\n', ' ').trim()
        val text = if (msg.isEmpty()) e.javaClass.simpleName else "${e.javaClass.simpleName}: $msg"
        return text.take(REASON_LIMIT)
    }

    // 不用 InputStream.readNBytes：那是 API 33+（minSdk 29 上会 NoSuchMethodError）。
    private fun readHead(s: InputStream?, limit: Int): String {
        if (s == null) return ""
        return s.use {
            val buf = ByteArray(limit)
            var off = 0
            while (off < limit) {
                val n = it.read(buf, off, limit - off)
                if (n <= 0) break
                off += n
            }
            String(buf, 0, off, Charsets.UTF_8)
        }
    }
}
