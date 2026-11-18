package dev.cnal.m0

/**
 * 界面要读的测量状态。M0 不引入绑定/IPC：服务与界面同进程，用 volatile 字段
 * 共享即可（测量壳的复杂度预算要留给 A1–A5，不留给架构）。
 */
object M0State {
    @Volatile var running = false
    @Volatile var bound = false
    @Volatile var boundLabel = "未绑定"
    @Volatile var v4 = "—"
    @Volatile var v6 = "—"
    @Volatile var lastLine = "尚未探测"
    @Volatile var beats = 0
    @Volatile var missed = 0
}
