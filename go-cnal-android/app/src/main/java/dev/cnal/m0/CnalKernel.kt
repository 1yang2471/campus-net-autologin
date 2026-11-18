package dev.cnal.m0

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import dev.cnal.mobile.CredentialSink
import dev.cnal.mobile.Mobile
import java.io.File
import java.security.KeyStore
import java.time.ZoneId
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Go 内核（AAR）的唯一装配点。Kotlin 壳只调这里，不直接碰 Mobile——
 * 注入顺序、凭据解密、面板页加载集中一处，避免服务/Activity 各拼一份。
 *
 * 生命周期：start() → stateJSON()/login()/... → stop()。
 * 凭据纪律：密码密文在 app 私有 SharedPreferences，密钥在 AndroidKeyStore
 * 不可导出；明文只在内存里交给 Go，任何文件/日志都不落密码。
 */
object CnalKernel {
    private const val PREFS = "cnal-credentials"
    private const val KEY_ALIAS = "cnal-aes-gcm"
    private const val KS = "AndroidKeyStore"
    private const val TRANSFORM = "AES/GCM/NoPadding"
    private const val KEY_ACCOUNTS = "accounts"
    private const val KEY_CURRENT = "current"

    @Volatile var panelUrl: String = ""
        private set
    @Volatile var lastError: String = ""
        private set

    /**
     * 写最小可用 config.ini（与 Go 侧 WriteStarterConfig 同形）。已存在时
     * 只更新 server 行，绝不覆盖用户手改的其它键。
     */
    fun saveServer(context: Context, server: String) {
        val s = server.trim()
        require(isValidServer(s)) { "门户地址必须是 http(s):// 开头" }
        val ini = File(context.filesDir, "config.ini")
        if (ini.exists()) {
            val lines = ini.readLines().map {
                if (it.trimStart().startsWith("server")) "server = $s" else it
            }
            ini.writeText(lines.joinToString("\n") + "\n")
            return
        }
        ini.writeText(
            "; M1 首跑生成（端点路径是校事实，异校请核对 README）\n" +
                "[keeper]\n" +
                "server = $s\n" +
                "login_path = /ac_portal/login.php\n" +
                "login_page = /ac_portal/default/pc.html?tabs=pwd\n" +
                "; campus_dns = 10.x.x.x   ; 已知校园 DNS 时填上\n",
        )
    }

    /**
     * 保存账号密码（产品首屏/面板保存调用；密文落私有 prefs，明文不落盘）。
     * 多账号：同用户名替换密文并置顶，上限 5 条——面板 /accounts 切换到的
     * 账号，其密码必须在启动时可重新注入 Go。
     */
    fun saveCredentials(context: Context, user: String, pass: String) {
        require(user.isNotBlank() && pass.isNotBlank()) { "账号或密码为空" }
        val (entries, _) = loadEntries(context)
        val (iv, ct) = seal(pass)
        val next = AccountVault.upsert(entries, VaultEntry(user.trim(), iv, ct))
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
            .putString(KEY_ACCOUNTS, AccountVault.encode(next))
            .putString(KEY_CURRENT, user.trim())
            .remove("user").remove("iv").remove("ct") // 旧单槽已迁移
            .apply()
    }

    /**
     * 纯函数，便于 JVM 单测：只接受 http(s) 门户基址。
     * 这里**不**拒绝私网/环回 host——门户地址是用户显式配置的校事实，
     * 校园门户本身就常在 10.x 内网段；程序自动构造/发现的 URL（L1 锚点）
     * 才由 Go 侧 AnchorPolicy 做公网校验，两条路径不混。
     */
    fun isValidServer(s: String): Boolean =
        s.startsWith("http://") || s.startsWith("https://")

    /** 读回当前账号明文（只在内存里，调用方随即交给 Go）。没有凭据返回 null。 */
    fun credentials(context: Context): Pair<String, String>? {
        val (entries, current) = loadEntries(context)
        val e = AccountVault.pick(entries, current) ?: return null
        return e.user to open(e)
    }

    /**
     * 全部账号明文，按「当前账号最后」的顺序返回：启动时逐条注入 Go 的
     * 内存凭据库（memoryCredentials），面板 /accounts 切换才有密码可用。
     * 单条解密失败跳过——不因一个坏条目让守护起不来。
     */
    fun allCredentials(context: Context): List<Pair<String, String>> {
        val (entries, current) = loadEntries(context)
        return AccountVault.injectionOrder(entries, current).mapNotNull { e ->
            runCatching { e.user to open(e) }.getOrNull()
        }
    }

    /** 当前账号（面板/首屏回填）：ini 的 username 优先，退回账本的 current。 */
    fun savedUser(context: Context): String {
        val ini = File(context.filesDir, "config.ini")
        if (ini.exists()) {
            ini.readLines()
                .firstOrNull { it.trimStart().startsWith("username") }
                ?.substringAfter("=")?.trim()
                ?.takeIf { it.isNotEmpty() }
                ?.let { return it }
        }
        val (entries, current) = loadEntries(context)
        return AccountVault.pick(entries, current)?.user.orEmpty()
    }

    /** 读账本：新格式优先；没有则从旧单槽（user/iv/ct）迁移一条。 */
    private fun loadEntries(context: Context): Pair<List<VaultEntry>, String> {
        val p = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        p.getString(KEY_ACCOUNTS, null)?.let { raw ->
            return AccountVault.decode(raw) to (p.getString(KEY_CURRENT, "") ?: "")
        }
        val legacy = AccountVault.migrateLegacy(
            p.getString("user", null), p.getString("iv", null), p.getString("ct", null))
        return legacy to (p.getString("user", "") ?: "")
    }

    /** AES-GCM 加密，返回 (iv, ct) 的 Base64。密钥在 AndroidKeyStore，不可导出。 */
    private fun seal(pass: String): Pair<String, String> {
        val c = Cipher.getInstance(TRANSFORM)
        c.init(Cipher.ENCRYPT_MODE, key())
        val ct = c.doFinal(pass.toByteArray(Charsets.UTF_8))
        return Base64.encodeToString(c.iv, Base64.NO_WRAP) to
            Base64.encodeToString(ct, Base64.NO_WRAP)
    }

    private fun open(e: VaultEntry): String {
        val c = Cipher.getInstance(TRANSFORM)
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, Base64.decode(e.iv, Base64.NO_WRAP)))
        return String(c.doFinal(Base64.decode(e.ct, Base64.NO_WRAP)), Charsets.UTF_8)
    }

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance(KS).apply { load(null) }
        val existing = ks.getKey(KEY_ALIAS, null) as? SecretKey
        if (existing != null) return existing
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KS)
        gen.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                // 守护要无人值守：不要求用户生物认证/锁屏解锁。
                .build()
        )
        return gen.generateKey()
    }

    /**
     * 装配内核并起 tick 循环。返回面板 URL（http://127.0.0.1:<port>/panel?token=…），
     * 失败抛出带原因的异常（不静默吞掉——M0 的教训是"没绑网"和"明文被拦"
     * 在日志里一模一样）。
     */
    fun start(context: Context, bound: NetBinder.Bound): String {
        val dir = context.filesDir.absolutePath
        Mobile.setDataDir(dir)
        // 免费时段判定要本地时钟：gomobile 进程无 TZ，Go time.Now 挂 UTC
        //（2026-11-11 首跑实证：UTC 下午被判进 23:30-06:30 → 不自动登录）。
        Mobile.setTZOffsetMin(TzOffset.minutesOf(ZoneId.systemDefault()).toLong())
        Mobile.setPanelPage(readPanelPage(context))
        // 面板里保存/修改的账号必须回到 Keystore，否则只活在 Go 内存，
        // 重启后 /accounts 切换会因「凭据库无此账号」被拒。
        Mobile.setCredentialSink(object : CredentialSink {
            override fun save(user: String, pass: String) {
                saveCredentials(context.applicationContext, user, pass)
            }
        })
        // 全量注入（当前账号最后）：Go 的内存凭据库是 /accounts 切换的唯一
        // 密码来源；只注入当前账号会让历史账号切换全部失败。
        val all = allCredentials(context)
        if (all.isEmpty()) {
            throw IllegalStateException("未配置账号密码：请先在设置里保存")
        }
        for ((u, p) in all) Mobile.setCredentials(u, p)
        injectSources(bound)
        val ini = File(dir, "config.ini")
        if (!ini.exists()) {
            throw IllegalStateException("缺少 config.ini：请把门户配置放进 app 私有目录")
        }
        val url = Mobile.start(ini.absolutePath)
        panelUrl = url
        lastError = ""
        return url
    }

    /**
     * 只注入物理源、不起内核（A4 绑网核对用：核对比启动守护轻得多，
     * 也不要求已配凭据）。v4 必须剥 /prefix；v6 从全列表挑 GUA——第一个
     * v6 常是 fe80 链路本地，只看 bound.v6 会漏掉真 GUA（2026-11-12 实证）。
     */
    fun injectSources(bound: NetBinder.Bound) {
        val v4 = PhysicalSource.ip(bound.v4)
        val v6 = pickV6Source(bound)
        Mobile.setPhysicalSources(v4, v6)
    }

    /** 注入用 v6 源：全列表挑 GUA，剥 /prefix；无 GUA 返回空串 = 只 v4。 */
    internal fun pickV6Source(bound: NetBinder.Bound): String =
        PhysicalSource.v6(bound.v6All)?.let { PhysicalSource.ip(it) } ?: ""

    /** 读 config.ini 里已保存的门户地址（界面回填；读不到返回空串）。 */
    fun savedServer(context: Context): String {
        val ini = File(context.filesDir, "config.ini")
        if (!ini.exists()) return ""
        return ini.readLines()
            .firstOrNull { it.trimStart().startsWith("server") }
            ?.substringAfter("=")?.trim()
            .orEmpty()
    }

    fun stop() {
        Mobile.stop()
        panelUrl = ""
    }

    /** VPN 暂停/恢复自动登录（A4 降级解）。只改内核内存开关，不写 ini。 */
    fun setAutoReconnect(on: Boolean) = Mobile.setAutoReconnect(on)

    fun state(): String = Mobile.stateJSON()
    fun login() = Mobile.login()
    fun logs(): String = Mobile.logsJSON()

    /**
     * A4 绑网核对（只读、无凭据）：用注入的物理源向门户问「你看到我的地址是什么」。
     * Go native socket + LocalAddr 绑源——与 M1 登录同一条路。
     * insecure = 配置里的 ssl_verify 取反（安卓系统信任库不含校园 CA，见 config 说明）。
     */
    fun egress(loginUrl: String, insecure: Boolean): String = Mobile.egressCheck(loginUrl, insecure)

    /** 读 config.ini 的 ssl_verify（缺省 true = 正常校验，与 Go 侧默认一致）。 */
    fun sslVerify(context: Context): Boolean {
        val ini = File(context.filesDir, "config.ini")
        if (!ini.exists()) return true
        val v = ini.readLines()
            .firstOrNull { it.trimStart().startsWith("ssl_verify") }
            ?.substringAfter("=")?.trim()?.lowercase()
        return v != "false" && v != "0" && v != "no"
    }

    /** 面板页由 go-cnal 侧唯一出处 panelPage() 生成，作为 assets 打进 APK。 */
    private fun readPanelPage(context: Context): String =
        context.assets.open("panel.html").use { it.bufferedReader().readText() }
}
