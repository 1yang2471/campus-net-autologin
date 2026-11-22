package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * 门户探测失败提示（D3，验收记录 android-a1-a5-result）：Go 侧逐锚点诊断串
 * （词表唯一出处 go-cnal/internal/app/detect.go）→ 首屏 Toast 的可行动文案。
 * 已认证时探测必失败（无 302 劫持可探）——用户要看到的是「不用探」，不是
 * 一串原始错误。
 */
class DetectHintTest {
    @Test
    fun authenticatedAnchorMeansAlreadyOnline() {
        // 已认证网络锚点返回真实应答（无劫持）：探测必失败，提示应为「无需探测」
        assertEquals(
            "当前网络已认证在线，无需探测——直接点「打开面板」看守护状态",
            DetectHint.hint("未探测到校园门户。逐锚点实测：223.5.5.5: 204（已认证的正常应答）"))
    }

    @Test
    fun authenticatedBeatsUnreachable() {
        // 混合诊断（一个锚点已认证 + 一个不可达）：只要有一个锚点拿到真实应答，
        // 网络就是通的——已认证优先于不可达。
        assertEquals(
            "当前网络已认证在线，无需探测——直接点「打开面板」看守护状态",
            DetectHint.hint("未探测到校园门户。逐锚点实测：" +
                "223.5.5.5: 不可达；119.29.29.29: 204（已认证的正常应答）"))
    }

    @Test
    fun whitelistPassthroughMeansAmbiguous() {
        // 白名单放行（未认证时也拿到真页面）：探测分不出已认证/未认证
        assertEquals(
            "锚点被校园白名单放行，探测分不出是否已认证——点「打开面板」看守护状态",
            DetectHint.hint("未探测到校园门户。逐锚点实测：223.5.5.5 Host=connect.rom.miui.com: " +
                "200（未认证时也返回真页面，疑被放行）"))
    }

    @Test
    fun unreachableMeansCheckWifi() {
        // 锚点全不可达：先确认连的是校园网
        assertEquals(
            "锚点不可达：确认 Wi-Fi 连的是校园网再探（开着梯子先关掉）",
            DetectHint.hint("未探测到校园门户。逐锚点实测：223.5.5.5: 不可达；119.29.29.29: 不可达"))
    }

    @Test
    fun missingSourceMeansBindFailed() {
        assertEquals(
            "Wi-Fi 未绑定成功：连上校园 Wi-Fi 后稍候再探",
            DetectHint.hint("未注入物理 v4 源：先绑 Wi-Fi 再探测（物理栈纪律，不猜出口）"))
        assertEquals(
            "Wi-Fi 未绑定成功：连上校园 Wi-Fi 后稍候再探",
            DetectHint.hint("无可用物理 v4 源（TUN 占满？）——物理栈纪律下不触网"))
    }

    @Test
    fun unknownMessagePassesThrough() {
        // 词表漂移兜底：未知诊断原样透传，信息不丢
        val raw = "未来词表漂移的新诊断串"
        assertEquals(raw, DetectHint.hint(raw))
    }
}
