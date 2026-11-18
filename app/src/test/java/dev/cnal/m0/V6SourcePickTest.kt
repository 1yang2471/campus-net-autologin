package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test

// v6 注入缺陷回归（2026-11-12 真机实证）：wlan0 LinkAddresses 顺序为
// [fe80::, v4, GUA稳定, GUA临时]，NetBinder.addrOf(lp,6) 只回第一个 v6
// = fe80 链路本地 → PhysicalSource.v6 拒掉 → 注入空 → Go 报 no_address，
// 而系统里 GUA 明明存在（/state v6_state=no_address 与 ip -6 addr 矛盾）。
// 修复 = Bound 携带完整 v6 地址列表，注入前从列表挑 GUA。
class V6SourcePickTest {
    // 真机 2026-11-12 dumpsys connectivity 抓取的 wlan0 地址序列（保序）。
    private val bound = NetBinder.Bound(
        bound = true, label = "Wi-Fi 已绑定（已验证）", v4 = "172.29.153.170/16",
        v6 = "fe80::7cde:a435:1f28:ceec/64",
        v6All = listOf(
            "fe80::7cde:a435:1f28:ceec/64",
            "2001:250:3003:aa29:cb32:e8da:3636:4112/64",
            "2001:250:3003:aa29:9b7d:42f1:1d5b:cfde/64",
        ),
    )

    @Test
    fun picksGuaFromFullListNotFirstV6() {
        assertEquals("2001:250:3003:aa29:cb32:e8da:3636:4112",
            CnalKernel.pickV6Source(bound))
    }

    @Test
    fun noGuaMeansEmptyInjection() {
        val linkLocalOnly = bound.copy(v6All = listOf("fe80::1/64"))
        assertEquals("", CnalKernel.pickV6Source(linkLocalOnly))
    }
}
