package dev.cnal.m0

/**
 * 首屏适用范围说明（公开发布门禁）。一句诚实的话，不是检测：
 * - 检测要触网、要维护各校指纹，是另一个功能；
 * - 这里只把"仅 ac_portal 族、仅参考校验证、其它学校大概率用不了"放在
 *   用户第一眼能看到的地方，省得公开用户装了发现登录不上来提 issue。
 */
object SupportNotice {
    fun text(portalConfigured: Boolean = true): String {
        val base = "仅适配 ac_portal 族门户，已在本校验证；其它学校大概率不可用，请先核对协议族。"
        return if (portalConfigured) base else "$base 使用前请先设置门户地址。"
    }
}
