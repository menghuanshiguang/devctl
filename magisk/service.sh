#!/system/bin/sh
# devctl agent: 开机自启 + 崩溃自动拉起 (10s)
MODDIR=${0%/*}
chmod 755 "$MODDIR/agent" 2>/dev/null
mkdir -p /data/local/devctl
while true; do
  "$MODDIR/agent" -port 5556 >> /data/local/devctl/agent.log 2>&1
  sleep 10
done &
