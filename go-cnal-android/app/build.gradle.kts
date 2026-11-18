plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// M0 只回答「壳能不能活」，不接 Go 内核：minSdk 29（android-plan §7 的
// -androidapi 29 同值），targetSdk 35（Play 16KB 截止 2027-02-01 对齐）。
android {
    namespace = "dev.cnal.m0"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.cnal.m0"
        minSdk = 29
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // release 签名：keystore 在仓库外，口令只在 keyring/环境变量，绝不进源码。
    // 缺任一环境变量即让 release 构建失败（不回落 debug 签名——公开发布时
    // debug 签名任何人可冒充）。构建前由 PowerShell 从 Windows Credential
    // Manager 的 CNAL_SIGN_KEY 条目读出口令注入环境变量。
    signingConfigs {
        create("release") {
            val store = System.getenv("CNAL_SIGN_STORE")
            val storePass = System.getenv("CNAL_SIGN_STOREPASS")
            val keyPass = System.getenv("CNAL_SIGN_KEYPASS")
            if (store != null && storePass != null && keyPass != null) {
                storeFile = file(store)
                storePassword = storePass
                keyAlias = "cnal-release"
                keyPassword = keyPass
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // 只赋签名引用，不在这里抛异常——否则 debug 测试/构建也会被挡。
            if (signingConfigs.getByName("release").storeFile != null) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

// release 签名护栏：只在 release 打包任务真正执行时检查，debug/测试不受影响。
// 缺 env 时用 debug 签名出 release 包是公开发布事故（任何人可冒充），必须硬拦。
tasks.matching { it.name.startsWith("assembleRelease") || it.name.startsWith("lintVitalRelease") }
    .configureEach {
        doFirst {
            if (System.getenv("CNAL_SIGN_STOREPASS") == null ||
                System.getenv("CNAL_SIGN_KEYPASS") == null) {
                throw GradleException(
                    "release 签名未配置：设置 CNAL_SIGN_STORE / CNAL_SIGN_STOREPASS / CNAL_SIGN_KEYPASS 后重试（或走 scripts/build-release.ps1）")
            }
        }
    }

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    // M1：gomobile bind 产出的 AAR（Go 内核）。由 go-cnal 侧
    // `gomobile bind -target=android/arm64 -androidapi 29 -javapkg dev.cnal
    //  -o app/libs/mobile.aar ./mobile` 生成；v1 只出 arm64（android-plan §7）。
    implementation(files("libs/mobile.aar"))
    testImplementation("junit:junit:4.13.2")
}
