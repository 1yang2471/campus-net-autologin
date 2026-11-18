package dev.cnal.m0

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * 首屏路由（纯逻辑）：产品首屏应该是面板，不是 M0 测量仪表。
 * 规则：
 *  1. autostart extra 优先（A1 无人值守测量不能被面板劫走）；
 *  2. 守护在跑且面板 URL 已就绪 → 直接进面板；
 *  3. 其余（未启动/启动失败）→ 产品落地页。
 */
class LaunchRouteTest {
    @Test
    fun autostartAlwaysGoesHome() {
        // A1 测量：adb 起 Activity，面板不得抢占（面板 URL 即凭据，测量流程
        // 不该把用户直接带进 WebView）。
        assertEquals(LaunchRoute.Screen.HOME,
            LaunchRoute.decide(autostart = true, kernelRunning = true, panelUrl = "http://127.0.0.1:1/panel"))
        assertEquals(LaunchRoute.Screen.HOME,
            LaunchRoute.decide(autostart = true, kernelRunning = false, panelUrl = ""))
    }

    @Test
    fun runningWithUrlGoesToPanel() {
        assertEquals(LaunchRoute.Screen.PANEL,
            LaunchRoute.decide(autostart = false, kernelRunning = true, panelUrl = "http://127.0.0.1:41467/panel?token=x"))
    }

    @Test
    fun runningWithoutUrlStaysHome() {
        // 服务活着但内核没起来（绑定失败/凭据缺失）：不能进空白面板。
        assertEquals(LaunchRoute.Screen.HOME,
            LaunchRoute.decide(autostart = false, kernelRunning = true, panelUrl = ""))
        assertEquals(LaunchRoute.Screen.HOME,
            LaunchRoute.decide(autostart = false, kernelRunning = false, panelUrl = "http://127.0.0.1:1/panel"))
    }

    @Test
    fun kernelRunningButProcessFreshStaysHome() {
        // 进程刚重启（前台服务被系统拉起）而 Activity 先创建：panelUrl 静态
        // 变量为空，必须停在落地页等 kernelLoop 填充，不能闪一下空面板。
        assertEquals(LaunchRoute.Screen.HOME,
            LaunchRoute.decide(autostart = false, kernelRunning = true, panelUrl = ""))
    }
}