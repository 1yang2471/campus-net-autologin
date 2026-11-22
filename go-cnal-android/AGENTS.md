<!-- claimrail:begin -->
## claimrail

动界面、文档、接口之前，先跑 `claimrail skill` 读协议（它从安装包自身打印，不依赖本仓库的 node_modules），
写 `.claimrail/decisions.json` 申报分叉与证据；
交付前跑 `claimrail check`——结构不合规先修申报，红的不许交。
命令不存在时：到 claimrail 源码目录 npm link，或用 node 直接跑它的 src/cli.mjs。
<!-- claimrail:end -->

## 测试约定

- 测试命令：`./gradlew :app:testDebugUnitTest --offline`（Kotlin 单元测试，JVM 层，不需要设备）
- 构建：`./gradlew :app:assembleDebug --offline`；AAR 由 go-cnal 侧重 bind 生成（完整出处 `app/build.gradle.kts` 顶部注释）：
  `ANDROID_NDK_HOME=D:\Android\sdk\ndk\28.2.13676358 gomobile bind -target=android/arm64 -androidapi 29 -javapkg dev.cnal -o ../go-cnal-android/app/libs/mobile.aar ./mobile`
  （缺 `-androidapi 29` 会报 `unsupported API version 16`；Git Bash 下 `ANDROID_NDK_HOME` 默认未设须带上）
- 提交前两条都要跑过；AAR 变更（Go 侧导出函数变动）必须先重 bind 再构建

