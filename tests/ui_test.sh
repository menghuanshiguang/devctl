#!/bin/bash
# ui_test.sh — devctl 悬浮窗自动化测试 (大企业流程: 构建→部署→截图→断言→报告)
# 用法: ./tests/ui_test.sh {deploy|shot|status|pull|full}
set -e
DEV="${DEV:-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SHOT_DIR="/data/local/devctl/ui_test"
LOCAL_SHOT="$ROOT/tests/shots"
mkdir -p "$LOCAL_SHOT"
log() { echo "[ui_test] $*"; }

deploy() {
  log "推送构建产物到设备..."
  python3 "$ROOT/client/devctl.py" run $DEV push "$ROOT/ui/dist/devui.dex" /data/local/tmp/devctl/devui.dex
  python3 "$ROOT/client/devctl.py" run $DEV push "$ROOT/ui/dist/libdevui_hide.so" /data/local/tmp/devctl/libdevui_hide.so
  python3 "$ROOT/client/devctl.py" run $DEV push "$ROOT/ui/dist/devfont.bin" /data/local/tmp/devctl/devfont.bin
  log "启动悬浮窗..."
  python3 "$ROOT/client/devctl.py" devui $DEV start || log "start 返回非零 (可能已在跑)"
  sleep 3
  log "部署完成"
}

shot() {
  log "设备截图..."
  python3 "$ROOT/client/devctl.py" run $DEV shell "mkdir -p $SHOT_DIR && screencap -p $SHOT_DIR/$(date +%H%M%S).png"
  python3 "$ROOT/client/devctl.py" run $DEV shell "ls -t $SHOT_DIR | head -1"
}

status() {
  log "devui 状态:"
  python3 "$ROOT/client/devctl.py" devui $DEV status
  log "agent 日志尾部:"
  python3 "$ROOT/client/devctl.py" run $DEV shell "tail -5 /data/local/devctl/agent.log"
  log "devui 日志尾部:"
  python3 "$ROOT/client/devctl.py" run $DEV shell "tail -5 /data/local/devctl/devui.log"
}

pull_latest() {
  local f
  f=$(python3 "$ROOT/client/devctl.py" run $DEV shell "ls -t $SHOT_DIR | head -1" | tr -d '\r')
  log "拉取 $f ..."
  python3 "$ROOT/client/devctl.py" run $DEV pull "$SHOT_DIR/$f" "$LOCAL_SHOT/$f"
  echo "$LOCAL_SHOT/$f"
}

full() { deploy; shot; sleep 1; status; pull_latest; log "完成. 截图在 $LOCAL_SHOT/" }

case "${1:-help}" in
  deploy) deploy ;;
  shot) shot ;;
  status) status ;;
  pull) pull_latest ;;
  full) full ;;
  *) echo "用法: $0 {deploy|shot|status|pull|full}" ;;
esac
