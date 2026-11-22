package dev.cnal.m0

/**
 * 门户探测失败的可行动文案（D3）：Go 侧 DetectPortal 的逐锚点诊断串 →
 * 首屏 Toast 用户能行动的一句话。已认证时探测必失败（无 302 劫持可探），
 * 用户要看到的是「不用探」，不是一串原始错误。
 *
 * 词表唯一出处：go-cnal/internal/app/detect.go（「已认证的正常应答」等判定词）。
 * Go 词表漂移时兜底透传原始 message，信息不丢；词表加词时改这里一行即可。
 */
object DetectHint {
    /** 诊断串 → 可行动文案。优先级：已认证 > 疑被放行 > 不可达 > 未注入源 > 透传。 */
    fun hint(message: String): String = when {
        "已认证的正常应答" in message ->
            "当前网络已认证在线，无需探测——直接点「打开面板」看守护状态"
        "疑被放行" in message ->
            "锚点被校园白名单放行，探测分不出是否已认证——点「打开面板」看守护状态"
        "不可达" in message ->
            "锚点不可达：确认 Wi-Fi 连的是校园网再探（开着梯子先关掉）"
        "未注入物理 v4" in message || "无可用物理 v4 源" in message ->
            "Wi-Fi 未绑定成功：连上校园 Wi-Fi 后稍候再探"
        else -> message
    }
}
