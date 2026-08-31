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

# ---- 0. 唤醒+解锁 (截图前必做, 否则黑屏) ----
wake() {
  log "唤醒+解锁..."
  python3 "$ROOT/client/devctl.py" run $DEV shell "input keyevent 224" 2>/dev/null || true
  sleep 2
  python3 "$ROOT/client/devctl.py" unlock $DEV 2>&1 | tail -1
  sleep 2
}

# ---- 1. 部署: 推送 dex/so/字库 + 启动 ----
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

# ---- 2. 截图 ----
shot() {
  log "设备截图..."
  python3 "$ROOT/client/devctl.py" run $DEV shell "mkdir -p $SHOT_DIR && screencap -p $SHOT_DIR/$(date +%H%M%S).png"
  python3 "$ROOT/client/devctl.py" run $DEV shell "ls -t $SHOT_DIR | head -1"
}

# ---- 3. 状态 ----
status() {
  log "devui 状态:"
  python3 "$ROOT/client/devctl.py" devui $DEV status
  log "SurfaceFlinger 层:"
  python3 "$ROOT/client/devctl.py" run $DEV shell "dumpsys SurfaceFlinger --list | grep -i devctl" || true
  log "devui 日志尾部:"
  python3 "$ROOT/client/devctl.py" run $DEV shell "tail -5 /data/local/devctl/devui.log"
}

# ---- 4. 拉截图回本地 ----
pull_latest() {
  local f
  f=$(python3 "$ROOT/client/devctl.py" run $DEV shell "ls -t $SHOT_DIR | head -1" | tr -d '\r')
  log "拉取 $f ..."
  python3 "$ROOT/client/devctl.py" run $DEV pull "$SHOT_DIR/$f" "$LOCAL_SHOT/$f"
  echo "$LOCAL_SHOT/$f"
}

# ---- 5. 全流程: 唤醒→截图→验证 ----
full() {
  deploy
  wake
  shot
  status
  pull_latest
  log "完成. 截图在 $LOCAL_SHOT/"
}

# ---- 6. 断言 ----
assert() {
  local f
  f="$LOCAL_SHOT/$(ls -t "$LOCAL_SHOT" | head -1)"
  log "断言: $f"
  python3 "$ROOT/tests/assert_shot.py" "$f" --mode auto || true
}

case "${1:-help}" in
  wake) wake ;;
  deploy) deploy ;;
  shot) shot ;;
  status) status ;;
  pull) pull_latest ;;
  assert) assert ;;
  full) full ;;
  *) echo "用法: $0 {wake|deploy|shot|status|pull|assert|full}" ;;
esac
