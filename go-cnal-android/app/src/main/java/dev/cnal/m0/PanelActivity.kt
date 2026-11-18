package dev.cnal.m0

import android.app.Activity
import android.os.Build
import android.os.Bundle
import android.webkit.WebSettings
import android.webkit.WebView
import android.widget.FrameLayout
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

/**
 * M1 面板容器：系统 WebView 加载 Go 内核回路的 /panel?token=…。
 * 面板契约（/state /login /config /flux /accounts /rebindv6 /logs）与桌面
 * cnalw 完全同一份，Kotlin 侧只负责容器，不重画界面。
 *
 * cleartext：回路是 http://127.0.0.1，已在 network_security_config.xml
 * 白名单放行（M0 实测的 M1 地雷，见 docs/m0-findings.md §2）。
 */
class PanelActivity : Activity() {
    private lateinit var web: WebView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val url = intent.getStringExtra(EXTRA_URL)
        if (url.isNullOrBlank() || !url.startsWith("http://127.0.0.1:")) {
            // 面板 URL 即回路凭据：不接受外部传来的任意 URL，也不猜端口。
            finish()
            return
        }
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = false
            settings.allowContentAccess = false
            settings.cacheMode = WebSettings.LOAD_NO_CACHE
            // 只伺服本机回路：外部页面/重定向一律拦掉（WebView 不是浏览器）。
            webViewClient = LoopbackOnlyClient()
        }
        val root = FrameLayout(this).apply { addView(web) }
        setContentView(root)
        // Android 15 edge-to-edge：面板自己有 24px padding，这里再补系统栏，
        // 否则状态栏/导航栏会压住内容（android-plan §4.4）。
        ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        web.loadUrl(url)
    }

    override fun onDestroy() {
        if (::web.isInitialized) web.destroy()
        super.onDestroy()
    }

    @Deprecated("Back：面板内返回，无历史时退出")
    override fun onBackPressed() {
        if (::web.isInitialized && web.canGoBack()) web.goBack() else super.onBackPressed()
    }

    companion object {
        const val EXTRA_URL = "dev.cnal.m0.PANEL_URL"
    }
}
