pluginManagement {
    repositories {
        // 国内可达性：google() 走 dl.google.com（本机实测 4MB/s）；mavenCentral 慢时
        // 可换阿里云镜像——顺序即优先级，不改语义。
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "cnal-m0"
include(":app")
