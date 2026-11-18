#!/usr/bin/env bash
# 提交前文件大小闸：单个 staged 文件超过 20000 字节（约 5000 token）拒绝提交。
# 理由：agent 读到大文件会变笨（tidy §3）；按功能拆成几个文件再提交。
# 跳过 lockfile/生成物/二进制——agent 不会去读它们。
#
# 安装：cp scripts/check-file-size.sh .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
set -u

LIMIT=20000
fail=0
while IFS= read -r f; do
    [ -n "$f" ] && [ -f "$f" ] || continue
    case "$f" in
        *.lock|*lock.json|*.sum|*.mod|*/dist/*|*.exe|*.dll|*.png|*.ico|*.jpg|*.webp|*.zip|*.gz|*.pdf)
            continue ;;
    esac
    sz=$(wc -c < "$f" 2>/dev/null || echo 0)
    if [ "$sz" -gt "$LIMIT" ]; then
        echo "拒绝提交：$f 有 $sz 字节（上限 $LIMIT 字节 ≈ 5000 token）。" >&2
        echo "请按功能拆成几个文件再提交（tidy §3）。" >&2
        fail=1
    fi
done < <(git diff --cached --name-only --diff-filter=ACM)

exit "$fail"
