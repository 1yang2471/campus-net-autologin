package dev.cnal.m0

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings

/**
 * 唤醒锁策略（纯逻辑，JVM 可单测）。
 *
 * A1 实证：屏幕熄灭后 MIUI Doze 冻住 m0-probe 线程（23 拍 vs 正常 ~65 拍，
 * 19 分钟零拍），进程/服务/线程都在，只是不执行。PARTIAL_WAKE_LOCK 能
 * 让 CPU 在屏灭后继续跑 tick，但认证完成后持锁就是白白耗电——策略只
 * 在「认证进行中 + 屏幕熄灭 + 门户可达」时持锁，其余释放。
 *
 * 不持锁、不能突破 Doze 的场景（Mimosa/厂商深度冻结），由
 * shouldAskForWhitelist 交给用户手动加电池白名单。
 */
object PowerPolicy {
    enum class WakeDecision {
        /** 已认证或无登录动作，释放锁省电。 */
        RELEASE,
        /** 认证进行中，屏灭后保持 CPU 活跃。 */
        HOLD,
    }

    /**
     * 决策：当前应不应持 PARTIAL_WAKE_LOCK。
     *
     * - 在线/免认证时段：无登录动作 → 释放。
     * - 门户不可达：持锁也发不出请求 → 释放，等恢复。
     * - 认证进行中（离线/启动中/被拒）+ 屏灭 + 门户可达 → 持锁，别被 Doze 冻住。
     * - 屏幕亮着：Doze 不生效 → 释放。
     */
    fun decide(state: String, screenOn: Boolean, portalReachable: Boolean): WakeDecision {
        if (screenOn) return WakeDecision.RELEASE
        if (state == "在线" || state == "免认证时段") return WakeDecision.RELEASE
        if (!portalReachable) return WakeDecision.RELEASE
        // 离线 / 启动中 / 被拒：登录动作未完成，屏灭后 Doze 会冻住 tick。
        return WakeDecision.HOLD
    }

    /**
     * 是否应该提醒用户手动加电池白名单（一次性）。
     */
    fun shouldAskForWhitelist(state: String, screenOn: Boolean, portalReachable: Boolean,
                              whitelisted: Boolean): Boolean =
        whitelisted.not() &&
            decide(state, screenOn, portalReachable) == WakeDecision.HOLD

    /** 跳到「应用电池优化」设置页，用户手动把本 app 设为“不优化”。 */
    fun whitelistIntent(context: Context): Intent =
        Intent(whitelistAction()).setData(Uri.parse("package:" + context.packageName))

    /** 电池优化设置页的动作名（JVM 单测可直接断言，不碰 Uri）。 */
    fun whitelistAction(): String =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS!!
        } else {
            Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS!!
        }

    // For JVM unit tests without an Android context.
    fun whitelistIntent(): Intent = Intent(whitelistAction())
}