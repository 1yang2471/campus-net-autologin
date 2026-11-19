package dev.cnal.m0

import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.util.TypedValue
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

/**
 * 产品首屏：状态摘要 + 「启动 M1 守护」/「打开面板」/「开发者选项」。
 *
 * 路由规则（LaunchRoute，纯逻辑单测已过）：
 *  - autostart extra → 无人值守测量，不显示 UI，直接起服务并退出；
 *  - 守护在跑且面板 URL 已就绪 → 直达面板（WebView /panel？token=…）；
 *  - 其余（未启动/启动失败/进程刚重启）→ 留在落地页等状态填充。
 *
 * M0 测量仪表已搬进 DeveloperActivity（设置→开发者选项），不占用首屏。
 */
class MainActivity : Activity() {
    private val ui = Handler(Looper.getMainLooper())
    private lateinit var status: TextView
    private lateinit var server: EditText

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // A1 无人值守起表入口：adb am start -e autostart 1（服务 exported=false，
        // shell 起不了，只能借 Activity 自己起）。只在带该 extra 时自动起，
        // 不显示 UI，直接退出；正常手点「启动 M1 守护」路径零变化。
        if (intent?.getBooleanExtra("autostart", false) == true) {
            ContextCompat.startForegroundService(
                this, Intent(this, KeeperService::class.java).setAction(KeeperService.ACTION_START))
            finish()
            return
        }

        when (LaunchRoute.decide(
            autostart = false,
            kernelRunning = M0State.running,
            panelUrl = CnalKernel.panelUrl,
        )) {
            LaunchRoute.Screen.PANEL -> openPanel()
            LaunchRoute.Screen.HOME -> {
                setContentView(buildUi())
                askNotificationPermission()
            }
        }
    }

    private fun askNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val p = android.Manifest.permission.POST_NOTIFICATIONS
        if (checkSelfPermission(p) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(p), 1)
        }
    }

    private fun openPanel() {
        val url = CnalKernel.panelUrl
        if (url.isEmpty()) {
            Toast.makeText(this, "面板尚未就绪，先点「启动 M1 守护」", Toast.LENGTH_SHORT).show()
            setContentView(buildUi())
        } else {
            startActivity(Intent(this, PanelActivity::class.java)
                .putExtra(PanelActivity.EXTRA_URL, url))
        }
    }

    private fun buildUi(): ScrollView {
        val pad = dp(24)
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, pad, pad, pad)
        }

        root.addView(TextView(this).apply {
            text = getString(R.string.app_name)
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 24f)
        })
        root.addView(TextView(this).apply {
            text = "校园网自动登录守护（M1）"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
            setPadding(0, dp(4), 0, dp(12))
        })
        // 适用范围常驻首屏（公开发布门禁）：用户第一眼就知道只适配参考校。
        root.addView(TextView(this).apply {
            text = SupportNotice.text(portalConfigured = CnalKernel.savedServer(this@MainActivity).isNotEmpty())
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 12f)
            setTextColor(0xFF8B919B.toInt()) // 次要文字，不抢状态卡
            setPadding(0, 0, 0, dp(20))
        })

        status = TextView(this).apply {
            text = "点「刷新」读取守护状态"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
            setPadding(0, dp(12), 0, dp(12))
        }
        root.addView(status)

        root.addView(row(
            Button(this).apply {
                text = "刷新状态"
                setOnClickListener { refreshStatus() }
            },
            Button(this).apply {
                text = "启动 M1 守护"
                setOnClickListener {
                    ContextCompat.startForegroundService(
                        this@MainActivity,
                        Intent(this@MainActivity, KeeperService::class.java)
                            .setAction(KeeperService.ACTION_START_M1))
                    refreshStatus()
                }
            },
        ))
        root.addView(row(
            Button(this).apply {
                text = "打开面板"
                setOnClickListener { openPanel() }
            },
            Button(this).apply {
                text = "开发者选项（M0 测量）"
                setOnClickListener {
                    startActivity(Intent(this@MainActivity, DeveloperActivity::class.java))
                }
            },
        ))

        root.addView(TextView(this).apply {
            text = "门户与账号（首次保存后不再修改，凭据落 Android Keystore，明文不落盘）"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 12f)
            setPadding(0, dp(16), 0, dp(4))
        })

        // 免手填（2026-11-19）：未认证时门户会 302 劫持到登录页自报地址，
        // 与 PC 首配向导同一条探测链，只是搬到首屏一键。
        root.addView(row(
            Button(this).apply {
                text = "自动探测门户（免手填）"
                setOnClickListener { detectPortal(this) }
            },
        ))

        server = EditText(this).apply {
            hint = "门户地址（如 https://a.stu.edu.cn）"
            inputType = android.text.InputType.TYPE_TEXT_VARIATION_URI
            setText(CnalKernel.savedServer(this@MainActivity))
        }
        val user = EditText(this).apply { hint = "账号" }
        val pass = EditText(this).apply {
            hint = "密码"
            inputType = android.text.InputType.TYPE_CLASS_TEXT or
                android.text.InputType.TYPE_TEXT_VARIATION_PASSWORD
        }
        root.addView(server)
        root.addView(user)
        root.addView(pass)
        root.addView(row(
            Button(this).apply {
                text = "保存配置/凭据"
                setOnClickListener {
                    val msg = runCatching {
                        CnalKernel.saveServer(this@MainActivity, server.text.toString())
                        CnalKernel.saveCredentials(
                            this@MainActivity, user.text.toString(), pass.text.toString())
                        "配置与凭据已保存"
                    }.getOrElse { "保存失败：${it.message}" }
                    Toast.makeText(this@MainActivity, msg, Toast.LENGTH_SHORT).show()
                }
            },
        ))

        // Android 15 edge-to-edge（targetSdk 35）：内容从屏幕顶画起，主题自带的
        // 窗口标题条会整段压住首屏文字（真机取证 2026-11-19：标题/副标题被盖、
        // 说明首行削半截）。NoActionBar + 根视图补系统栏内边距（同 PanelActivity）。
        return ScrollView(this).apply {
            addView(root)
            ViewCompat.setOnApplyWindowInsetsListener(this) { v, insets ->
                val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
                v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
                insets
            }
        }
    }

    /**
     * 自动探测门户：绑 Wi-Fi → 注入物理源 → Go 无凭据 GET 探测（302 劫持页
     * 自报地址 + 登录页指纹复核）→ 回填并保存。后台线程执行（探测会触网、
     * 最坏数秒）；按钮在途锁定防重入。
     */
    private fun detectPortal(b: Button) {
        if (!b.isEnabled) return
        b.isEnabled = false
        val label = b.text
        b.text = "探测中…"
        Thread({
            val result = runCatching {
                val bound = NetBinder(this).bind()
                CnalKernel.detectPortal(this, bound)
            }
            ui.post {
                b.isEnabled = true
                b.text = label
                result
                    .onSuccess { url ->
                        server.setText(url)
                        val msg = runCatching {
                            CnalKernel.saveServer(this, url)
                            "已探测并保存门户：$url"
                        }.getOrElse { "已探测：$url（保存失败：${it.message}）" }
                        Toast.makeText(this, msg, Toast.LENGTH_LONG).show()
                    }
                    .onFailure { e ->
                        Toast.makeText(this, "探测失败：${e.message}", Toast.LENGTH_LONG).show()
                    }
            }
        }, "detect-portal").start()
    }

    private fun refreshStatus() {
        val snap = CnalKernel.state()
        val label = runCatching { org.json.JSONObject(snap).optString("label") }
            .getOrDefault("读取失败")
        val detail = runCatching { org.json.JSONObject(snap).optString("detail") }
            .getOrDefault("")
        status.text = "$label｜$detail"
    }

    private fun row(vararg buttons: Button): LinearLayout = LinearLayout(this).apply {
        orientation = LinearLayout.HORIZONTAL
        // 按钮高度自适应：定高 48dp 会裁掉换行按钮的第二行（真机实证），行高下限 48dp
        gravity = Gravity.CENTER_VERTICAL
        minimumHeight = dp(48)
        buttons.forEach { addView(it, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)) }
    }

    private fun dp(v: Int): Int =
        (v * resources.displayMetrics.density).toInt()
}
