#!/bin/bash
# ci_fetch.sh — 从 GitHub Actions 拉取最新 ui-dist 构建产物
# 用法: ./scripts/ci_fetch.sh [run_id]  (缺省用最近成功 build run)
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
RUN_ID="${1:-$(gh run list --workflow build.yml --limit 30 --json databaseId,conclusion --jq '[.[] | select(.conclusion=="success")][0].databaseId')}"
if [ -z "$RUN_ID" ]; then echo "[ci_fetch] 无成功构建"; exit 1; fi
echo "[ci_fetch] run $RUN_ID → 下载 ui-dist"
rm -rf /tmp/ci-ui-fetch
gh run download "$RUN_ID" -n ui-dist -D /tmp/ci-ui-fetch
mkdir -p ui/dist
cp /tmp/ci-ui-fetch/devui.dex /tmp/ci-ui-fetch/libdevui_hide.so /tmp/ci-ui-fetch/devfont.bin ui/dist/
chmod +x ui/dist/libdevui_hide.so
echo "[ci_fetch] 完成: ui/dist/"
ls -la ui/dist/
