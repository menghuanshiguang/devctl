#!/system/bin/sh
# devctl agent: 开机自启 + 崩溃自动拉起 (10s) + module.prop 显示当前 IP
MODDIR=${0%/*}
chmod 755 "$MODDIR/agent" 2>/dev/null
mkdir -p /data/local/devctl

# 更新 module.prop description → Magisk App 模块列表直接显示 IP
IP=$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.*src \([0-9.]*\).*/\1/p')
[ -z "$IP" ] && IP="offline"
sed -i "s/^description=.*/description=IP: $IP | devctl agent :5556/" "$MODDIR/module.prop" 2>/dev/null

while true; do
  "$MODDIR/agent" -port 5556 >> /data/local/devctl/agent.log 2>&1
  sleep 10
done &
