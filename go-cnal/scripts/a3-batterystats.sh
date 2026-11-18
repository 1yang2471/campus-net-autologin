#!/usr/bin/env bash
# A3 电耗采集（复盘 §3.1 观测台架配套）：起止电量 + batterystats 前后快照。
# 用法：
#   a3-batterystats.sh start   # 记起始电量 + 重置 batterystats，写快照到 OUTDIR
#   a3-batterystats.sh stop    # 记结束电量 + 拉取 batterystats 归因，写同目录
# 只读+reset：不动服务、不动网络、不动门户会话。USB adb 过夜（无线会掉）。
# 注意：若用 `dumpsys battery unplug` 伪造断电，结论须披露台架手段（复盘 §4）。
set -euo pipefail
export ANDROID_HOME="${ANDROID_HOME:-D:/Android/sdk}"
export PATH="$ANDROID_HOME/platform-tools:$PATH"
OUTDIR="${A3_OUTDIR:-D:/workspace/go-cnal/docs/a3-$(date '+%Y%m%d-%H%M%S')}"
PKG="dev.cnal.m0"

mkdir -p "$OUTDIR"
STAMP=$(date '+%Y-%m-%dT%H:%M:%S%z')

# 无设备时不报错：记录“设备未连接”并退出
require_device() {
  if [ -z "$(adb devices 2>/dev/null | tail -n +2 | grep -v '^$' | sed '/^List of devices attached/d')" ]; then
    {
      echo "=== A3 终点快照 $STAMP ==="
      echo "设备未连接"
    } | tee "$OUTDIR/battery-stop.txt"
    echo "输出目录: $OUTDIR"
    echo "设备未连接，已记录并退出"
    exit 0
  fi
}

case "${1:-}" in
start)
  require_device
  {
    echo "=== A3 起点快照 $STAMP ==="
    adb shell dumpsys battery | grep -E "level|status|powered|Charge counter"
    adb shell dumpsys batterystats --reset
    echo "（batterystats 已重置）"
  } | tee "$OUTDIR/battery-start.txt"
  echo "输出目录: $OUTDIR"
  ;;
stop)
  require_device
  {
    echo "=== A3 终点快照 $STAMP ==="
    adb shell dumpsys battery | grep -E "level|status|powered|Charge counter"
    echo
    echo "=== batterystats 归因（$PKG 相关段） ==="
    adb shell dumpsys batterystats "$PKG" | head -80
    echo
    echo "=== 服务存活（对照 A2） ==="
    adb shell dumpsys activity services "$PKG" | head -20
  } | tee "$OUTDIR/battery-stop.txt"
  echo "输出目录: $OUTDIR"
  echo "判读：心跳功耗 %/h = (起点电量-终点电量)/时长h ×100%，≤3%/h 为 A3 通过线"
  ;;
*)
  echo "用法: $0 start|stop" >&2
  exit 1
  ;;
esac
