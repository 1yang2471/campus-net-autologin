// M0 测量 APK（纯 Kotlin，不接 Go 内核）：只回答"前台服务能不能活 + 绑网能不能
// 钉住 Wi-Fi + 30s 探针有没有漏拍"。见 go-cnal/docs/android-plan.md §5.1。
plugins {
    id("com.android.application") version "8.6.1" apply false
    id("org.jetbrains.kotlin.android") version "2.0.21" apply false
}
