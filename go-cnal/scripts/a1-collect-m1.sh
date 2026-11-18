#!/usr/bin/env bash
# A1 第 3 轮收表（M1 内核版）：等手机拔线 → 从拔线时刻起 30 分钟 → 收表。
# 只读：不动服务、不动网络、不动门户会话；唯一的“主动”动作是收表时经
# adb forward 打一次本机回路的 /state（验内核 30 分钟屏灭后是否可响应）。
export ANDROID_HOME="D:/Android/sdk"
export PATH="$ANDROID_HOME/platform-tools:$PATH"
OUT="D:/workspace/go-cnal/docs/a1-probe-2026-11-12-m1-whitelist.log"
BASE=$(date '+%Y-%m-%dT%H:%M:%S%z')
echo "=== A1 M1 白名单轮 收表脚本启动 $BASE ==="

# 1) 等拔线（默认最多 90 分钟，可用 A1_UNPLUG_WAIT_MIN 覆盖，每 10s 探一次）。
#    整夜没拔就按「充电过夜观察」收表并在原文里如实记 AC 状态——充电轮
#    不构成 A1 结论，只作补充观察。
WAIT_MIN=${A1_UNPLUG_WAIT_MIN:-90}
for i in $(seq 1 $((WAIT_MIN * 6))); do
  AC=$(adb shell dumpsys battery 2>/dev/null | grep -i "^  AC powered" | awk '{print $3}')
  if [ "$AC" != "true" ]; then break; fi
  sleep 10
done
UNPLUGGED_AT=$(date '+%Y-%m-%dT%H:%M:%S%z')
AC_AFTER=$(adb shell dumpsys battery 2>/dev/null | grep -i "^  AC powered" | awk '{print $3}')
{
  echo "=== A1 M1 白名单轮（拔线时刻起算） ==="
  echo "脚本启动: $BASE"
  echo "拔线确认: $UNPLUGGED_AT (AC powered=$AC_AFTER)"
  echo "白名单: $(adb shell dumpsys deviceidle whitelist | grep -i cnal)"

  echo "--- 屏灭计时 30 分钟 $(date '+%T') ---"
  sleep 1800

  echo "--- 电源（须全 false 才算有效）---"
  adb shell dumpsys battery | grep -iE "AC powered|USB powered|Wireless powered|status|level"
  echo "--- 屏幕/Doze ---"
  adb shell dumpsys power | grep -iE "mWakefulness=" | head -2
  adb shell dumpsys deviceidle | grep -iE "mState=|mLightState=" | head -2
  echo "--- 白名单 ---"
  adb shell dumpsys deviceidle whitelist | grep -i cnal
  echo "--- 服务/进程 ---"
  adb shell pidof dev.cnal.m0
  adb shell dumpsys activity services dev.cnal.m0 2>/dev/null | grep -iE "isForeground|startRequested" | head -3
  echo "--- 唤醒锁（按策略在线时不持，应为空）---"
  adb shell dumpsys power | grep -i "cnal-auth-wake" || echo "(无 cnal 唤醒锁)"
  echo "--- 内核文件 mtime（应晚于拔线时刻）---"
  adb shell run-as dev.cnal.m0 ls -l files/api.json files/cnalw.log
  echo "--- 内核回路 /state（屏灭 30 分钟后内核是否可响应）---"
  API=$(adb shell run-as dev.cnal.m0 cat files/api.json 2>/dev/null)
  echo "$API"
  PORT=$(echo "$API" | sed -n 's/.*127.0.0.1:\([0-9]*\).*/\1/p')
  TOKEN=$(echo "$API" | sed -n 's/.*"token":"\([0-9a-f]*\)".*/\1/p')
  if [ -n "$PORT" ] && [ -n "$TOKEN" ]; then
    adb forward tcp:$PORT tcp:$PORT >/dev/null 2>&1
    curl -s -m 6 -H "Authorization: Bearer $TOKEN" http://127.0.0.1:$PORT/state && echo || echo "EMPTY/TIMEOUT（内核冻结或不可达）"
    adb forward --remove tcp:$PORT >/dev/null 2>&1
  fi
  echo "--- cnalw.log 尾部 40 行 ---"
  adb shell run-as dev.cnal.m0 cat files/cnalw.log 2>/dev/null | tail -40
} > "$OUT" 2>&1
echo "collected -> $OUT"
