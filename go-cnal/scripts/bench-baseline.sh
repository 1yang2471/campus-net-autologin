#!/usr/bin/env bash
# 性能基线采集（复盘 §3.5）：热路径基准 + 构建物体积，落 docs/bench-baseline-<日期>.txt。
# 用法：bash scripts/bench-baseline.sh
# 判读：与上一份基线逐项对比（ns/op、B/op、allocs/op）；分配数经 TestParseAllocBudget
# 闸在 go test 里自动守。体积对比三个 exe 的字节数。
set -euo pipefail
cd "$(dirname "$0")/.."
OUT="docs/bench-baseline-$(date '+%Y-%m-%d').txt"
{
  echo "=== go-cnal 性能基线 $(date '+%Y-%m-%d %H:%M:%S %z') ==="
  echo
  echo "--- 热路径基准（go test -bench . -benchmem -run '^$'） ---"
  go test ./internal/dialect/ ./internal/protocol/ -bench . -benchmem -run '^$' 2>&1 | grep -vE "^ok|no test files"
  echo
  echo "--- 构建物体积（存在才记；build.ps1 产出） ---"
  for f in cnal.exe cnald.exe cnalw.exe; do
    if [ -f "$f" ]; then
      echo "$(stat -c %s "$f" 2>/dev/null || wc -c <"$f") bytes  $f"
    else
      echo "（未构建）      $f"
    fi
  done
} | tee "$OUT"
echo
echo "基线已写入 $OUT"
