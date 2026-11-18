#!/usr/bin/env bash
# A1 M1 心跳轮收表（2026-11-12 复盘 §3.1 台架配套）。
# 前提：M1 守护已在跑（界面已点「启动 M1 守护」）、用户已拔电+熄屏。
# 流程：确认拔电 → 睡满窗口（默认 30 分钟）→ 等 adb 回连（无线 adb 深
# Doze 会掉，点亮手机即恢复）→ 拉 cnalw.log + /state + 电池状态 → 算拍间隔。
# 只读手机文件；不动服务/网络/门户会话。踢线由用户在窗口内自行操作。
#
# 用法：bash scripts/a1-collect-m1-heartbeat.sh
#   A1_DURATION_MIN=30       窗口分钟数
#   A1_DEV=172.29.153.170:5555  无线 adb 地址（USB 连接时忽略）
#   A1_OUT=docs/...          输出文件
#   A1_SCHEDULED=1           定时模式：跳过拔电等待（信任窗口约定，如凌晨
#                            定时跑）；收表前按 A1_RECONNECT_WAIT_MIN 等回连
#   A1_RECONNECT_WAIT_MIN=240  回连等待分钟数（默认 240，等用户点亮手机）
set -u
export ANDROID_HOME="${ANDROID_HOME:-D:/Android/sdk}"
export PATH="$ANDROID_HOME/platform-tools:$PATH"
DEV="${A1_DEV:-172.29.153.170:5555}"
DUR_MIN="${A1_DURATION_MIN:-30}"
OUT="${A1_OUT:-docs/a1-probe-$(date '+%Y-%m-%d')-m1-heartbeat.log}"
SCHEDULED="${A1_SCHEDULED:-0}"
RECONNECT_WAIT_MIN="${A1_RECONNECT_WAIT_MIN:-240}"

echo "=== A1 M1 心跳轮 $(date '+%Y-%m-%dT%H:%M:%S%z') 启动 ==="

# 1) 等拔电（AC powered false；充电轮不算 A1）。交互模式最多等 60 分钟；
#    定时模式跳过（凌晨手机息屏 adb 本就不可达，信任窗口约定，事后以
#    收表时的电池状态披露核实）。
if [ "$SCHEDULED" != "1" ]; then
  for i in $(seq 1 120); do
    AC=$(adb shell dumpsys battery 2>/dev/null | grep -i "^  AC powered" | awk '{print $3}')
    [ "$AC" = "false" ] && break
    sleep 30
  done
  AC=$(adb shell dumpsys battery 2>/dev/null | grep -i "^  AC powered" | awk '{print $3}')
  if [ "$AC" != "false" ]; then
    echo "警告：始终未拔电（AC powered=$AC），本轮按充电观察收表，不构成 A1 结论" >&2
  fi
fi
START=$(date '+%Y-%m-%dT%H:%M:%S%z')
echo "窗口起点 $START，时长 $DUR_MIN 分钟"

# 2) 睡满窗口（期间手机应保持熄屏、不点亮、不充电）。
sleep $((DUR_MIN * 60))

# 3) 等 adb 回连（深 Doze 无线 adb 会掉；用户点亮手机后恢复。定时模式
#    无人值守，最长等 RECONNECT_WAIT_MIN 分钟——等用户早上醒来点亮）。
for i in $(seq 1 $((RECONNECT_WAIT_MIN * 6))); do
  if adb devices | grep -q "	device$"; then break; fi
  adb connect "$DEV" >/dev/null 2>&1
  sleep 10
done
adb devices | grep -q "	device$" || echo "警告：adb 未回连，以下内容可能不完整（手机点亮后重跑本脚本收表）" >&2

# 4) 收表：日志全量 + /state + 电池。日志每拍一行，文件在手机侧持久化。
{
  echo "=== 窗口 $START + ${DUR_MIN}min；收表 $(date '+%Y-%m-%dT%H:%M:%S%z') ==="
  echo
  echo "--- 电池（收表时） ---"
  adb shell dumpsys battery 2>/dev/null | grep -E "level|AC powered|USB powered|status"
  echo
  echo "--- cnalw.log 全量 ---"
  adb shell run-as dev.cnal.m0 cat files/cnalw.log 2>/dev/null
  echo
  echo "--- /state（回路快照） ---"
  API=$(adb shell run-as dev.cnal.m0 cat files/api.json 2>/dev/null)
  URL=$(echo "$API" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p')
  TOK=$(echo "$API" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
  if [ -n "$URL" ] && [ -n "$TOK" ]; then
    PORT=$(echo "$URL" | sed -n 's/.*:\([0-9]*\)$/\1/p')
    adb forward "tcp:$PORT" "tcp:$PORT" >/dev/null 2>&1
    curl -s -m 5 -H "Authorization: Bearer $TOK" "$URL/state" 2>/dev/null
    echo
  else
    echo "（api.json 不可读，跳过 /state）"
  fi
} > "$OUT" 2>&1

echo
echo "--- 拍间隔（窗口内心跳行） ---"
grep "INFO 拍:" "$OUT" | awk '{
  d=$1; t=$2; gsub(/[-:]/," ",d); gsub(/:/," ",t); ts=mktime(d" "t)
  if (NR==1) { print "首拍 " $1 " " $2; prev=ts; prevs=$1 " " $2; next }
  printf "%s → %s  (%d s)\n", prevs, $1 " " $2, ts-prev; prev=ts; prevs=$1 " " $2
}'
echo
echo "收表已写入 $OUT"
echo "判读：兜底拍应 5min±容差一次；事件（Wi-Fi 断/回）应即拍；"
echo "      踢线恢复延迟 = 用户记录的踢线时刻 vs 日志「状态: 在线」时刻。"
