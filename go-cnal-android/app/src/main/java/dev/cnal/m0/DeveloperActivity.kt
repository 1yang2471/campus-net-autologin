package dev.cnal.m0

import android.Manifest
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
import java.io.File
import java.time.ZoneId

/**
 * 开发者选项（M0 测量仪表）：只做"看得见 + 点得动"——服务状态、绑网结果、
 * 最近一拍、漏拍数、日志尾部。用代码搭视图（不引布局 XML）：M0 的界面是
 * 测量仪表，不是产品界面，所以不放在首屏，搬到这里。
 */
class DeveloperActivity : Activity() {
    private lateinit var status: TextView
    private lateinit var logView: TextView
    private lateinit var server: EditText
    private val ui = Handler(Looper.getMainLooper())

    private val store get() = ProbeStore(File(filesDir, "probe.log"))

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(buildUi())
        askNotificationPermission()
    }

    override fun onResume() {
        super.onResume()
        tick()
    }

    override fun onPause() {
        super.onPause()
        ui.removeCallbacksAndMessages(null)
    }

    private fun tick() {
        status.text = statusText()
        logView.text = store.tail(30).joinToString("\n").ifEmpty { "（日志为空）" }
        ui.postDelayed(::tick, 1000)
    }

    private fun statusText(): String = buildString {
        append("服务：").append(if (M0State.running) "运行中（前台服务 specialUse）" else "已停止").append('\n')
        append("绑网：").append(M0State.boundLabel).append('\n')
        append("v4：").append(M0State.v4).append('\n')
        append("v6：").append(M0State.v6).append('\n')
        append("拍数：").append(M0State.beats)
        append("　漏拍：").append(M0State.missed).append('\n')
        append("最近：").append(M0State.lastLine)
    }

    private fun buildUi(): ScrollView {
        val pad = dp(16)
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, pad, pad, pad)
        }

        root.addView(TextView(this).apply {
            text = getString(R.string.app_name)
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 20f)
        })
        root.addView(TextView(this).apply {
            text = "M0 只测「壳能不能活」：前台服务存活 + 绑 Wi-Fi + 30s 探针。" +
                "模拟器不受 Doze/OEM 阉割，本页数据不构成 A1–A5 结论。"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 12f)
        })

        status = TextView(this).apply {
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
            setPadding(0, dp(12), 0, dp(12))
        }
        root.addView(status)

        root.addView(row(
            Button(this).apply {
                text = "启动服务"
                setOnClickListener { start() }
            },
            Button(this).apply {
                text = "停止服务"
                setOnClickListener { stopService(Intent(this@DeveloperActivity, KeeperService::class.java)) }
            },
        ))
        root.addView(row(
            Button(this).apply {
                text = "立即探测"
                setOnClickListener { probeNow() }
            },
            Button(this).apply {
                text = "清空日志"
                setOnClickListener { store.clear(); tick() }
            },
        ))
        // A4 绑网核对：门户侧看到的出口 IP 必须等于 Wi-Fi 地址。两条路各测一次
        // （Kotlin socket / Go native socket），Go 那条才是 M1 登录实际走的。
        root.addView(Button(this).apply {
            text = "A4 出口核对（Kotlin / Go 两路）"
            setOnClickListener { a4Check() }
        })

        // M1：Go 内核入口。凭据只在 Keystore/内存，界面只收一次并立即加密落库。
        root.addView(TextView(this).apply {
            text = "M1 守护（Go 内核）"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 14f)
            setPadding(0, dp(16), 0, dp(4))
        })
        server = EditText(this).apply {
            hint = "门户地址（如 https://a.stu.edu.cn）"
            inputType = android.text.InputType.TYPE_TEXT_VARIATION_URI
            setText(CnalKernel.savedServer(this@DeveloperActivity)) // 已配置过就回填
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
                        CnalKernel.saveServer(
                            this@DeveloperActivity, server.text.toString())
                        CnalKernel.saveCredentials(
                            this@DeveloperActivity, user.text.toString(), pass.text.toString())
                        "配置与凭据已保存"
                    }.getOrElse { "保存失败：${it.message}" }
                    Toast.makeText(this@DeveloperActivity, msg, Toast.LENGTH_SHORT).show()
                }
            },
            Button(this).apply {
                text = "启动 M1 守护"
                setOnClickListener {
                    ContextCompat.startForegroundService(
                        this@DeveloperActivity,
                        Intent(this@DeveloperActivity, KeeperService::class.java)
                            .setAction(KeeperService.ACTION_START_M1))
                    tick()
                }
            },
        ))
        root.addView(Button(this).apply {
            text = "打开面板"
            setOnClickListener {
                val url = CnalKernel.panelUrl
                if (url.isEmpty()) {
                    Toast.makeText(this@DeveloperActivity,
                        "守护尚未启动（或启动失败，看通知）", Toast.LENGTH_SHORT).show()
                } else {
                    startActivity(Intent(this@DeveloperActivity, PanelActivity::class.java)
                        .putExtra(PanelActivity.EXTRA_URL, url))
                }
            }
        })

        root.addView(TextView(this).apply {
            text = "探针日志（尾部 30 行）"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 12f)
            setPadding(0, dp(12), 0, dp(4))
        })
        logView = TextView(this).apply {
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 11f)
            typeface = android.graphics.Typeface.MONOSPACE
        }
        root.addView(ScrollView(this).apply {
            addView(logView)
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, dp(260))
        })
        // 同 MainActivity：edge-to-edge 补系统栏内边距，防标题条/状态栏压内容
        // （2026-11-19 真机取证）。
        return ScrollView(this).apply {
            addView(root)
            ViewCompat.setOnApplyWindowInsetsListener(this) { v, insets ->
                val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
                v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
                insets
            }
        }
    }

    private fun row(vararg buttons: Button): LinearLayout = LinearLayout(this).apply {
        orientation = LinearLayout.HORIZONTAL
        // 同 MainActivity：高度自适应，行高下限 48dp，防换行按钮第二行被裁
        gravity = Gravity.CENTER_VERTICAL
        minimumHeight = dp(48)
        buttons.forEach { addView(it, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f)) }
    }

    private fun start() {
        ContextCompat.startForegroundService(
            this, Intent(this, KeeperService::class.java).setAction(KeeperService.ACTION_START))
        tick()
    }

    /** 手动拍一次：省掉等 30s，用来现场确认闸/网络/判定是否正常。 */
    private fun probeNow() {
        Thread {
            val ts = System.currentTimeMillis()
            val r = ProbeRunner.once(KeeperService.ANCHOR, KeeperService.TIMEOUT_MS)
            val line = ProbeLog.line(ts, r.code, r.ms, r.verdict, ZoneId.systemDefault(), r.reason)
            store.append(line)
            M0State.lastLine = line
            runOnUiThread { tick() }
        }.start()
    }

    /**
     * A4 绑网核对（`android-a1-a5-testplan.md` §4）：门户侧看到的出口 IP 必须等于
     * 绑定的 Wi-Fi 地址。两条路各测一次——Kotlin socket 与 **Go native socket**
     * （M1 登录走后者，官方只承诺 Java 层受 bindProcessToNetwork 影响，因此必须实测）。
     * 结果落进探针日志（同一份文件，A1–A5 记录模板直接引用）。
     */
    private fun a4Check() {
        val serverBase = server.text.toString().trim().ifEmpty { CnalKernel.savedServer(this) }
        Thread {
            val ts = System.currentTimeMillis()
            val b = NetBinder(this).bind()
            CnalKernel.injectSources(b)
            val wifi = PhysicalSource.ip(b.v4)
            val line = if (serverBase.isEmpty()) {
                ProbeLog.a4(ts, wifi, "未填门户地址", "未测", b.label, "SKIP", ZoneId.systemDefault())
            } else {
                // Kotlin 控制路走**明文 HTTP**：门户在 http 上就答同一个 JSON
                // （实测 `{'success':true,'ip':...,'isOnline':true}`），绕开
                // 「Java 默认信任库不含校园 CA」这个与 A4 无关的干扰项。
                val host = serverBase.removePrefix("https://").removePrefix("http://").trimEnd('/')
                val shellUrl = "http://" + host + "/ac_portal/login.php"
                val goUrl = serverBase.trimEnd('/') + "/ac_portal/login.php"
                val insecure = !CnalKernel.sslVerify(this)
                val shell = runCatching { A4Egress.portalSeenIpViaShell(shellUrl) }
                    .getOrElse { "失败(${it.message?.take(40)})" }
                val go = runCatching { A4Egress.portalSeenIpViaGo(goUrl, insecure) }
                    .getOrElse { "失败(${it.message?.take(40)})" }
                // 判定只看 **Go 那条路**（M1 登录实际走的）。Kotlin 控制路走明文 HTTP，
                // 会被本 app 的 network_security_config 拦下（实测 "Cleartext HTTP
                // traffic not permitted"）——那是我们自己的策略在生效，不算 A4 失败。
                val verdict = if (go == wifi) "PASS" else "FAIL"
                ProbeLog.a4(ts, wifi, shell, go, b.label, verdict, ZoneId.systemDefault())
            }
            store.append(line)
            M0State.lastLine = line
            runOnUiThread {
                tick()
                Toast.makeText(this@DeveloperActivity, line, Toast.LENGTH_LONG).show()
            }
        }.start()
    }

    private fun askNotificationPermission() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val p = Manifest.permission.POST_NOTIFICATIONS
        if (checkSelfPermission(p) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(p), 1)
        }
    }

    private fun dp(v: Int): Int =
        (v * resources.displayMetrics.density).toInt()
}
