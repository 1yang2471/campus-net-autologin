package dev.cnal.m0

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.os.SystemClock
import dev.cnal.mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.time.ZoneId

/**
 * M0 前台服务：只做四件事——活着、绑网、30s 探针、把每一拍落盘。
 *
 * 类型 `specialUse`：不在 Android 15 的 6h/24h 前台服务超时名单里，也不在
 * BOOT_COMPLETED 的禁启名单里（android-plan §4.1 的官方依据）。状态机、登录、
 * 双栈一概不做——那是 M1 接 Go 内核的事，M0 只回答"壳能不能活"。
 */
class KeeperService : Service() {
    companion object {
        const val ACTION_START = "dev.cnal.m0.START"
        // M1：起 Go 内核（AAR）与 WebView 面板；M0 的 START 原样保留给 A1–A5。
        const val ACTION_START_M1 = "dev.cnal.m0.START_M1"
        const val ACTION_STOP = "dev.cnal.m0.STOP"

        // 公网锚点：锚在私网/环回证明不了"到达真实互联网"（AnchorPolicy 会拦）。
        const val ANCHOR = "http://connect.rom.miui.com/generate_204"
        const val TIMEOUT_MS = 5000

        private const val CHANNEL_ID = "keeper"
        private const val NOTIF_ID = 1
        private const val FLUX_NOTIF_ID = 2 // 流量提醒独立通知：不覆盖常驻前台服务通知
        private const val STATE_POLL_MS = 5000L // 通知 5s 轮询，状态变了才刷（§4.1）
    }

    @Volatile private var alive = false
    private var worker: Thread? = null
    private var kernelWorker: Thread? = null
    private var wakeLock: PowerManager.WakeLock? = null
    private lateinit var store: ProbeStore
    private lateinit var binder: NetBinder
    // 事件驱动（复盘 §3.2）：NetworkCallback → Mobile.wake 打断 Go 兜底等待。
    private var netCallback: ConnectivityManager.NetworkCallback? = null
    private var lastWakeAt = 0L // 节流：距上次唤醒 < MIN_GAP_MS 吞掉
    private var lastCaps: WakeCaps? = null
    // A4 降级解（2026-11-18）：VPN 开着时 Go socket 被 TUN 整体抓走，守护会误判
    // 掉线→反复自动登录撞校园冻结线。VPN 边沿→暂停/恢复自动登录（探测展示照常）。
    private val vpnGuard = VpnGuard()

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        store = ProbeStore(File(filesDir, "probe.log"))
        binder = NetBinder(this)
        createChannel()
    }

    /** 注册网络回调（M1 起内核时）：链路生死必投，capabilities 过 diff，
     *  全部经 WakeFilter 节流后 Mobile.wake（非阻塞，通道满自动丢）。 */
    private fun registerNetCallback() {
        if (netCallback != null) return
        val cm = getSystemService(ConnectivityManager::class.java)
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onLost(network: Network) = wakeOnce("onLost")
            override fun onAvailable(network: Network) = wakeOnce("onAvailable")
            override fun onCapabilitiesChanged(
                network: Network, caps: NetworkCapabilities) {
                val snap = WakeCaps(
                    validated = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED),
                    wifi = caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI),
                    netId = network.networkHandle.hashCode())
                val changed = WakeFilter.capsChanged(lastCaps ?: snap, snap)
                lastCaps = snap
                if (changed) wakeOnce("onCapabilitiesChanged")
                // A4 降级解：每次 capabilities 变化都看一次 VPN 系统事实（钥匙图标）。
                onVpnFact(caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN))
            }
        }
        cm.registerDefaultNetworkCallback(cb)
        netCallback = cb
    }

    private fun unregisterNetCallback() {
        netCallback?.let {
            runCatching { getSystemService(ConnectivityManager::class.java).unregisterNetworkCallback(it) }
        }
        netCallback = null
        lastCaps = null
    }

    /** 用户原本的 auto_reconnect 设定（首次 VPN 事件时从 /state 采一次；
     *  不能被暂停态污染——所以只采一次，之后一直用它做恢复目标）。 */
    @Volatile private var userAutoReconnect: Boolean? = null

    private fun onVpnFact(vpnPresent: Boolean) {
        if (userAutoReconnect == null) {
            userAutoReconnect = runCatching {
                org.json.JSONObject(CnalKernel.state())
                    .optJSONObject("extra")?.optBoolean("auto_reconnect", true) ?: true
            }.getOrDefault(true)
        }
        val target = userAutoReconnect ?: return // 防御性：上面刚赋值，正常不可能为空
        when (vpnGuard.onVpnChanged(vpnPresent, target)) {
            VpnGuard.Action.PAUSE -> {
                CnalKernel.setAutoReconnect(false)
                notify("梯子已开，守护暂停自动登录（探测照常，避免误判掉线反复登录）")
            }
            VpnGuard.Action.RESUME -> {
                CnalKernel.setAutoReconnect(true)
                notify("梯子已关，守护恢复自动登录")
            }
            VpnGuard.Action.NONE -> Unit
        }
    }

    /** 节流后投递唤醒；SystemClock 单调时钟，不受改表影响。 */
    private fun wakeOnce(reason: String) {
        val now = SystemClock.elapsedRealtime()
        if (WakeFilter.throttled(reason, lastWakeAt, now, WakeFilter.MIN_GAP_MS)) return
        lastWakeAt = now
        Mobile.wake(reason)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            CnalKernel.stop()
            stopSelf()
            return START_NOT_STICKY
        }
        startAsForeground()
        M0State.running = true
        when (intent?.action) {
            ACTION_START_M1 -> {
                if (kernelWorker == null) {
                    alive = true
                    registerNetCallback() // 事件驱动唤醒（复盘 §3.2）
                    kernelWorker = Thread(::kernelLoop, "m1-state").also { it.start() }
                }
            }
            else -> {
                // M0 测量循环：A1–A5 的判定依据，原样保留。
                if (!alive) {
                    alive = true
                    worker = Thread(::loop, "m0-probe").also { it.start() }
                }
            }
        }
        // START_STICKY：被系统/ROM 杀掉后让系统重新拉起——A2 要观测的正是"它会不会被
        // 杀、杀了会不会回来"，所以这里不能返回 START_NOT_STICKY。
        return START_STICKY
    }

    override fun onDestroy() {
        wakeLock?.release()
        wakeLock = null
        alive = false
        worker?.interrupt()
        worker = null
        kernelWorker?.interrupt()
        kernelWorker = null
        unregisterNetCallback()
        CnalKernel.stop()
        M0State.running = false
        super.onDestroy()
    }

    /** 屏灭 + 认证进行中 + 门户可达 → 持 PARTIAL_WAKE_LOCK，否则释放省电。
     *  holdMs 为已连续持有毫秒（kernelLoop 累计）：单拍判 HOLD 但连挂到
     *  WakeHoldPolicy 上限也放——复盘 §3.3「屏灭+被拒无限期持锁」的主动防线；
     *  acquire(timeout) 是兜底（策略逻辑失效时系统到点自动放）。 */
    private fun syncWakeLock(state: String, portalReachable: Boolean, holdMs: Long) {
        val pm = getSystemService(PowerManager::class.java)
        val screenOn = pm.isInteractive
        val decision = WakeHoldPolicy.decide(
            hold = wakeLock != null, holdMs = holdMs,
            limitMs = WakeHoldPolicy.DEFAULT_LIMIT_MS, pollMs = STATE_POLL_MS,
            base = PowerPolicy.decide(state, screenOn, portalReachable))
        if (decision == PowerPolicy.WakeDecision.HOLD.name && wakeLock == null) {
            wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "cnal-auth-wake")
            wakeLock!!.acquire(WakeHoldPolicy.DEFAULT_LIMIT_MS + STATE_POLL_MS)
            notify("保活锁已开启：屏灭后继续尝试登录（加电池白名单可更稳定）")
        } else if (decision == PowerPolicy.WakeDecision.RELEASE.name && wakeLock != null) {
            wakeLock!!.release()
            wakeLock = null
        }
        if (decision == PowerPolicy.WakeDecision.HOLD.name) {
            maybeAskWhitelist(pm, screenOn, portalReachable)
        }
    }

    private var whitelistReminded = false

    /** 提醒用户手动把本 app 加到电池优化白名单（提醒一次，不自杀网络）。 */
    private fun maybeAskWhitelist(pm: PowerManager, screenOn: Boolean, portalReachable: Boolean) {
        if (whitelistReminded) return
        if (!PowerPolicy.shouldAskForWhitelist(
                "离线", screenOn, portalReachable,
                pm.isIgnoringBatteryOptimizations(packageName))) {
            return
        }
        val shared = getSharedPreferences("cnal-prefs", MODE_PRIVATE)
        if (shared.getBoolean("whitelist_asked", false)) {
            whitelistReminded = true
            return
        }
        shared.edit().putBoolean("whitelist_asked", true).apply()
        whitelistReminded = true
        notify("守护可能被省电策略冻结：请在系统设置→应用→CNAL M0→电池中把“优化”关掉")
    }

    /** M1：绑 Wi-Fi → 起 Go 内核 → 5s 轮询状态刷通知（状态不变不刷屏）。 */
    private fun kernelLoop() {
        var last = ""
        var wakeHeldMs = 0L // 连续持锁累计：到 WakeHoldPolicy 上限即放（复盘 §3.3）
        val fluxGate = FluxNotice.Gate() // 进程生命周期内每档只提醒一次
        while (alive) {
            try {
                val b = binder.bind()
                M0State.bound = b.bound
                M0State.boundLabel = b.label
                M0State.v4 = b.v4
                M0State.v6 = b.v6
                if (CnalKernel.panelUrl.isEmpty()) {
                    val url = CnalKernel.start(this, b)
                    notify("守护已启动 · 面板就绪")
                    last = "started:$url"
                }
                val snap = CnalKernel.state()
                val label = runCatching { JSONObject(snap).optString("label") }
                    .getOrDefault("")
                val detail = runCatching { JSONObject(snap).optString("detail") }
                    .getOrDefault("")
                // 唤醒锁：屏灭 + 认证进行中 + 门户可达 → 持锁，别让 Doze
                // 冻住 tick（A1 实证）。连挂上限（复盘 §3.3）与 acquire
                // (timeout) 双保险：策略漏了系统也会到点自动放。
                val wasHeld = wakeLock != null
                syncWakeLock(label, portalReachable = label != "认证服务器不可达",
                    holdMs = wakeHeldMs)
                wakeHeldMs = if (wakeLock != null) {
                    if (wasHeld) wakeHeldMs + STATE_POLL_MS else 0L
                } else 0L
                val line = "$label｜$detail"
                if (line != last && label.isNotEmpty()) {
                    notify(line)
                    last = line
                }
                // 流量越阈：内核 /state 带 flux.level，这里发一次性非阻断通知。
                // 不改自动连、不断网；Gate 保证每档只发一次（kernelLoop 每 5s 轮询）。
                val lvl = FluxNotice.level(snap)
                if (fluxGate.shouldNotify(lvl)) {
                    val r = FluxNotice.readings(snap)
                    notifyFlux(FluxNotice.title(lvl),
                        FluxNotice.body(r.percent, r.used, r.quota, r.hasPercent))
                }
            } catch (e: Exception) {
                // 失败必须带原因：M0 的教训是"没绑网"和"明文被拦"在日志里
                // 长得一模一样，静默吞掉就查不动。
                val msg = "内核启动/轮询失败：${e.message}"
                notify(msg)
                last = msg
            }
            try {
                Thread.sleep(STATE_POLL_MS)
            } catch (e: InterruptedException) {
                break
            }
        }
    }

    private fun loop() {
        var lastBeat = System.currentTimeMillis() // 第一拍不参与漏拍统计
        while (alive) {
            val started = System.currentTimeMillis()
            M0State.missed += Cadence.missed(lastBeat, started)
            lastBeat = started

            val b = binder.bind() // 每拍重绑：网络切换后旧绑定会失效（官方警告）
            M0State.bound = b.bound
            M0State.boundLabel = b.label
            M0State.v4 = b.v4
            M0State.v6 = b.v6

            val r = ProbeRunner.once(ANCHOR, TIMEOUT_MS)
            val line = ProbeLog.line(started, r.code, r.ms, r.verdict, ZoneId.systemDefault(), r.reason)
            store.append(line)
            M0State.lastLine = line
            M0State.beats += 1
            notify(line)

            val rest = Cadence.PERIOD_MS - (System.currentTimeMillis() - started)
            if (rest > 0) {
                try {
                    Thread.sleep(rest)
                } catch (e: InterruptedException) {
                    break // 停服务时立刻退出
                }
            }
        }
    }

    private fun createChannel() {
        val mgr = getSystemService(NotificationManager::class.java)
        val ch = NotificationChannel(CHANNEL_ID, getString(R.string.channel_name),
            NotificationManager.IMPORTANCE_LOW)
        mgr.createNotificationChannel(ch)
    }

    private fun startAsForeground() {
        val n = Notification.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle(getString(R.string.notif_title))
            .setContentText("正在测量（30s 探针）")
            .setOngoing(true)
            .build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIF_ID, n)
        }
    }

    private fun notify(line: String) {
        val n = Notification.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle(getString(R.string.notif_title))
            .setContentText(line)
            .setOngoing(true)
            .build()
        getSystemService(NotificationManager::class.java).notify(NOTIF_ID, n)
    }

    /** 一次性流量提醒：独立通知 ID、非 ongoing，不覆盖常驻前台服务通知。 */
    private fun notifyFlux(title: String, body: String) {
        val n = Notification.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle(title)
            .setContentText(body)
            .setOngoing(false)
            .setAutoCancel(true)
            .build()
        getSystemService(NotificationManager::class.java).notify(FLUX_NOTIF_ID, n)
    }
}
