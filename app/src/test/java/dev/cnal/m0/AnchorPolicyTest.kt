package dev.cnal.m0

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

// 锚点闸：探针目标必须是"真实互联网"上的公网主机。落在环回/私网/保留地址上
// 既证明不了联网，也是 SSRF 的经典落点。
class AnchorPolicyTest {
    @Test
    fun publicHostsAreAllowed() {
        assertTrue(AnchorPolicy.allows("connect.rom.miui.com"))
        assertTrue(AnchorPolicy.allows("223.5.5.5")) // 阿里 DNS：Go 版 probe_ips 同款
        assertTrue(AnchorPolicy.allows("119.29.29.29"))
    }

    @Test
    fun loopbackAndLocalhostAreRejected() {
        assertFalse(AnchorPolicy.allows("localhost"))
        assertFalse(AnchorPolicy.allows("foo.localhost"))
        assertFalse(AnchorPolicy.allows("127.0.0.1"))
        assertFalse(AnchorPolicy.allows("127.8.8.8"))
        assertFalse(AnchorPolicy.allows("[::1]"))
    }

    @Test
    fun privateRangesAreRejected() {
        assertFalse(AnchorPolicy.allows("10.0.0.1"))
        assertFalse(AnchorPolicy.allows("172.16.0.1"))
        assertFalse(AnchorPolicy.allows("172.31.255.254"))
        assertFalse(AnchorPolicy.allows("192.168.1.1"))
        assertFalse(AnchorPolicy.allows("100.64.0.1")) // CGNAT
    }

    @Test
    fun linkLocalAndMetadataAreRejected() {
        assertFalse(AnchorPolicy.allows("169.254.169.254")) // 云元数据经典落点
        assertFalse(AnchorPolicy.allows("[fe80::1]"))
    }

    @Test
    fun garbageIsRejected() {
        assertFalse(AnchorPolicy.allows(null))
        assertFalse(AnchorPolicy.allows(""))
        assertFalse(AnchorPolicy.allows("   "))
        assertFalse(AnchorPolicy.allows("999.1.1.1")) // 非法点分：不当域名放行
    }

    // 行为：172.16-172.31 是私有，172.32 不是——边界不许一刀切成 /8。
    @Test
    fun privateBoundaryIsNotBroad() {
        assertTrue(AnchorPolicy.allows("172.32.0.1"))
        assertTrue(AnchorPolicy.allows("11.0.0.1"))
    }
}
