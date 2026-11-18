package dev.cnal.m0

/**
 * 首屏路由（纯逻辑）：产品首屏应该是面板，不是 M0 测量仪表。
 *
 * 三条规则，按优先级：
 *  1. autostart extra 优先——A1 无人值守测量（adb am start -e autostart 1）
 *     不得被面板劫走；面板 URL 即凭据，测量流程不该把用户直接带进 WebView。
 *  2. 守护在跑且面板 URL 已就绪 → 直接进面板。
 *  3. 其余（未启动/启动失败/进程刚重启）→ 产品落地页。
 *
 * 为什么第 3 条要单独拦住：进程刚重启时前台服务可能已被系统拉起
 * （kernelRunning=true），但 kernelLoop 还没跑、panelUrl 静态变量是空的——
 * 此时进面板就是一片空白。必须停在落地页等状态填充。
 */
object LaunchRoute {
    enum class Screen { HOME, PANEL }

    fun decide(autostart: Boolean, kernelRunning: Boolean, panelUrl: String): Screen =
        if (autostart) Screen.HOME
        else if (kernelRunning && panelUrl.startsWith("http://127.0.0.1:")) Screen.PANEL
        else Screen.HOME
}