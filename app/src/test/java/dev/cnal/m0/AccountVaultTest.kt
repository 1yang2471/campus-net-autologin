package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 多账号凭据账本（纯逻辑）：编码/去重/上限/迁移。加密与 Keystore 在
 * CnalKernel，需要 Android 运行环境；这里保证账本本身可测。
 *
 * 背景：旧实现只有单槽（prefs 的 user/iv/ct 三个键），面板 /accounts 切换
 * 到第二个账号时 Go 内存里没有密码 → 切换被拒。现在 Kotlin 落多条密文，
 * 启动时全量注入 Go。
 */
class AccountVaultTest {
    private fun e(user: String, tag: String = "x") = VaultEntry(user, "iv-$tag", "ct-$tag")

    @Test
    fun roundTripPreservesUsernames() {
        val entries = listOf(e("20230001"), e("工号/2024+abc"), e("a b@c"))
        val raw = AccountVault.encode(entries)
        assertEquals(entries, AccountVault.decode(raw))
    }

    @Test
    fun decodeSkipsCorruptLines() {
        val raw = AccountVault.encode(listOf(e("alice"), e("bob"))) + "\ngarbage-line\n"
        assertEquals(listOf("alice", "bob"), AccountVault.decode(raw).map { it.user })
    }

    @Test
    fun upsertReplacesSameUserAndMovesToFront() {
        val list = AccountVault.upsert(listOf(e("alice"), e("bob")), e("bob", "new"))
        assertEquals(listOf("bob", "alice"), list.map { it.user })
        assertEquals("ct-new", list.first().ct)
    }

    @Test
    fun upsertCapsAtFiveDroppingOldest() {
        var list = emptyList<VaultEntry>()
        for (u in listOf("u1", "u2", "u3", "u4", "u5", "u6")) {
            list = AccountVault.upsert(list, e(u))
        }
        assertEquals(AccountVault.MAX, list.size)
        assertEquals(listOf("u6", "u5", "u4", "u3", "u2"), list.map { it.user })
    }

    @Test
    fun pickFallsBackToFirstWhenCurrentMissing() {
        val entries = listOf(e("alice"), e("bob"))
        assertEquals("alice", AccountVault.pick(entries, "gone")?.user)
        assertEquals("bob", AccountVault.pick(entries, "bob")?.user)
    }

    @Test
    fun injectionOrderPutsCurrentLast() {
        val entries = listOf(e("alice"), e("bob"), e("carol"))
        val order = AccountVault.injectionOrder(entries, "bob")
        assertEquals("bob", order.last().user)
        assertEquals(setOf("alice", "bob", "carol"), order.map { it.user }.toSet())
    }

    @Test
    fun migrateLegacySingleSlot() {
        val got = AccountVault.migrateLegacy("alice", "iv", "ct")
        assertEquals(listOf(VaultEntry("alice", "iv", "ct")), got)
        assertTrue(AccountVault.migrateLegacy(null, "iv", "ct").isEmpty())
        assertTrue(AccountVault.migrateLegacy("alice", null, "ct").isEmpty())
        assertTrue(AccountVault.migrateLegacy("alice", "iv", null).isEmpty())
    }
}
