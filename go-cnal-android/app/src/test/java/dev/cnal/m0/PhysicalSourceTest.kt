package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// 注入给 Go 内核的物理源：必须是纯 IP（不带 /prefix），v6 只取全局 GUA。
// M0 实测模拟器只有 fe80:: 链路本地——照抄"第一个 v6 地址"会把链路本地
// 地址当成 GUA 注进去，握手恒失败（android-plan §3.4 / A5）。
class PhysicalSourceTest {
    @Test
    fun stripsCidrSuffixFromV4() {
        assertEquals("10.0.2.16", PhysicalSource.ip("10.0.2.16/24"))
        assertEquals("172.29.1.5", PhysicalSource.ip("172.29.1.5/16"))
    }

    @Test
    fun linkLocalV6IsNotAGua() {
        assertNull(PhysicalSource.v6(listOf("fe80::1/64", "10.0.2.16/24")))
    }

    @Test
    fun globalGuaIsPickedOverLinkLocal() {
        assertEquals("2001:db8::1",
            PhysicalSource.v6(listOf("fe80::1/64", "2001:db8::1/64", "10.0.2.16/24")))
    }

    @Test
    fun uniqueLocalAndLoopbackAreRejected() {
        assertNull(PhysicalSource.v6(listOf("fd00::1/64", "::1/128")))
        assertNull(PhysicalSource.v6(listOf("::1/128")))
    }

    @Test
    fun noV6ReturnsNull() {
        assertNull(PhysicalSource.v6(emptyList()))
        assertNull(PhysicalSource.v6(listOf("10.0.2.16/24")))
    }
}
