#!/usr/bin/env bash
# A1 第 2 轮收表：12:29:27 起的第一拍算，等到约 12:59:30 拉 probe.log 存证。
# 只读手机文件，不动服务、不动网络、不动门户会话。
export ANDROID_HOME="D:/Android/sdk"
export PATH="$ANDROID_HOME/platform-tools:$PATH"
OUT="D:/workspace/go-cnal/docs/a1-probe-2026-11-11-clean.log"
sleep 1800
{
  echo "=== A1 第 2 轮收表 $(date '+%Y-%m-%dT%H:%M:%S%z') ==="
  echo "--- 电源（须全 false 才算有效）---"
  adb shell dumpsys battery | grep -iE "AC powered|USB powered|Wireless powered|status|level"
  echo "--- 屏幕/Doze ---"
  adb shell dumpsys power | grep -iE "mWakefulness=" | head -2
  adb shell dumpsys deviceidle | grep -iE "mState=|mLightState=" | head -2
  echo "--- 服务 ---"
  adb shell dumpsys activity services dev.cnal.m0 | grep -iE "isForeground|startRequested" | head -2
  echo "--- probe.log 全文 ---"
  adb shell run-as dev.cnal.m0 cat files/probe.log
  echo "--- 行数 ---"
  adb shell run-as dev.cnal.m0 wc -l files/probe.log
} > "$OUT" 2>&1
echo "collected -> $OUT"
