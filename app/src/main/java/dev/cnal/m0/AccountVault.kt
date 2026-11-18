package dev.cnal.m0

import java.net.URLDecoder
import java.net.URLEncoder

/**
 * 一条账号密文：用户名 + AES-GCM 的 iv/ct（Base64）。密码明文不在这里，
 * 只由 CnalKernel 在内存里短期解密后交给 Go。
 */
data class VaultEntry(val user: String, val iv: String, val ct: String)

/**
 * 多账号凭据账本的纯逻辑：编码/去重/上限/迁移。Keystore 与加解密在
 * CnalKernel（需要 Android 运行环境）；这里只保证账本可单测。
 *
 * 存储格式：每行 `user\tiv\tct`；user 先做 URL 编码，避免用户名里的
 * 制表符/换行把账本拆坏。坏行跳过而不是整本作废——一条脏数据不该让
 * 用户丢掉其它账号。
 */
object AccountVault {
    /** 历史上限与桌面 [accounts] 段一致（MAX_ACCOUNTS=5）。 */
    const val MAX = 5

    fun encode(entries: List<VaultEntry>): String =
        entries.joinToString("\n") { e ->
            URLEncoder.encode(e.user, "UTF-8") + "\t" + e.iv + "\t" + e.ct
        }

    fun decode(raw: String?): List<VaultEntry> {
        if (raw.isNullOrBlank()) return emptyList()
        val out = ArrayList<VaultEntry>()
        for (line in raw.split('\n')) {
            val parts = line.split('\t')
            if (parts.size != 3 || parts.any { it.isEmpty() }) continue
            val user = runCatching { URLDecoder.decode(parts[0], "UTF-8") }
                .getOrNull() ?: continue
            out += VaultEntry(user, parts[1], parts[2])
        }
        return out
    }

    /** 同用户名视为同一条：替换密文并置顶（与桌面 RememberAccount 同语义）。 */
    fun upsert(entries: List<VaultEntry>, entry: VaultEntry): List<VaultEntry> =
        (listOf(entry) + entries.filter { it.user != entry.user }).take(MAX)

    /** 当前账号；记录缺失/过期时回退账本首条（不因一次脏写丢全部）。 */
    fun pick(entries: List<VaultEntry>, current: String): VaultEntry? =
        entries.firstOrNull { it.user == current } ?: entries.firstOrNull()

    /**
     * 注入 Go 的顺序：当前账号放最后。Go 的 memoryCredentials 以最后一条
     * 同时作为 legacy 单条回落——顺序错了会让「没写 username 的旧 ini」
     * 启动成别的账号。
     */
    fun injectionOrder(entries: List<VaultEntry>, current: String): List<VaultEntry> {
        val cur = pick(entries, current) ?: return entries
        return entries.filter { it.user != cur.user } + cur
    }

    /** 旧单槽（prefs 的 user/iv/ct 三键）迁移；缺任一字段返回空。 */
    fun migrateLegacy(user: String?, iv: String?, ct: String?): List<VaultEntry> {
        if (user.isNullOrBlank() || iv.isNullOrBlank() || ct.isNullOrBlank()) return emptyList()
        return listOf(VaultEntry(user, iv, ct))
    }
}
