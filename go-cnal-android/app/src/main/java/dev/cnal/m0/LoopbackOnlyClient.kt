package dev.cnal.m0

import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import java.net.URI

/**
 * WebView 只准访问本机回路面板。回路是 Go 内核的 127.0.0.1 服务器（设计契约
 * §2 / android-plan §4.4），不是对外服务端请求：这里把 host 严格钉在
 * 127.0.0.1，任何其他 host（含 localhost、环回别名、私网/保留地址、公网）
 * 以及非 http(s) scheme 一律拦掉，避免面板里的链接/重定向把 WebView 带出去。
 */
class LoopbackOnlyClient : WebViewClient() {
    override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
        // 返回 true = 由我们拒绝；只有回路允许 WebView 继续加载。
        return !isLoopbackPanel(request.url.toString())
    }

    override fun shouldInterceptRequest(
        view: WebView,
        request: WebResourceRequest,
    ): WebResourceResponse? {
        if (!isLoopbackPanel(request.url.toString())) {
            // 子资源也拦：面板 CSP 已禁外部资源，这里是第二道。
            return WebResourceResponse("text/plain", "utf-8", null)
        }
        return super.shouldInterceptRequest(view, request)
    }

    companion object {
        /** 纯 JDK 解析，便于 JVM 单测（不依赖 android.net.Uri 的 mock）。 */
        fun isLoopbackPanel(raw: String): Boolean {
            val u = try {
                URI(raw)
            } catch (e: Exception) {
                return false
            }
            if (u.scheme != "http" && u.scheme != "https") return false
            if (u.host != "127.0.0.1") return false // 明确只允许这一个 loopback
            return u.port > 0 // 端口由内核随机分配，必须显式存在
        }
    }
}
