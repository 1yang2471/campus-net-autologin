package dev.cnal.m0

import java.io.File

/** 探针日志落盘（M0 的原始证据）：追加写 + 尾部读回。 */
class ProbeStore(private val file: File) {
    fun append(line: String) {
        file.parentFile?.mkdirs()
        file.appendText(line + "\n")
    }

    fun tail(n: Int): List<String> {
        if (!file.exists()) return emptyList()
        return file.readLines().takeLast(n)
    }

    fun clear() {
        if (file.exists()) file.delete()
    }
}
