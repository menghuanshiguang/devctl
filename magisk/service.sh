#!/system/bin/sh
# devctl agent: 开机自启 + 崩溃自动拉起 (10s) + module.prop 显示当前 IP
# IPv6 兼容: 纯 IPv6 出网 (如 iPhone 热点 IPv6-only) 时也能取到地址
MODDIR=${0%/*}
chmod 755 "$MODDIR/agent" 2>/dev/null
mkdir -p /data/local/devctl

# 取本机 IP: 优先 IPv4, 无 IPv4 (纯 IPv6 网络) 时取全局 IPv6
# 输出: "1.2.3.4" 或 "2409:xxxx::1" 或 "offline"
get_ip() {
  # IPv4 主路由源地址 (只匹配点分十进制)
  IP=$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.*src \([0-9][0-9.]*\).*/\1/p')
  [ -n "$IP" ] && { echo "$IP"; return; }
  # IPv6 主路由源地址 (全局地址, 排除 %zone 和链路本地)
  IP=$(ip -6 route get 2001:4860:4860::8888 2>/dev/null | sed -n 's/.*src \([0-9a-fA-F:]*%*[0-9a-zA-Z]*\).*/\1/p' | sed 's/%[0-9a-zA-Z]*$//')
  [ -n "$IP" ] && echo "$IP" || echo "offline"
}

# 更新 module.prop description → Magisk App 模块列表直接显示 IP
# (开机早期网络未就绪, ip route get 失败 → offline; 循环内持续刷新)
update_ip() {
  IP=$(get_ip)
  sed -i "s/^description=.*/description=IP: $IP | devctl agent :5556/" "$MODDIR/module.prop" 2>/dev/null
}

# 前台等网络: 最多 30s (等到了立即写, 等不到也不阻塞 agent 启动)
i=0
while [ "$i" -lt 10 ]; do
  IP=$(get_ip)
  [ "$IP" != "offline" ] && break
  i=$((i+1))
  sleep 3
done
update_ip

while true; do
  update_ip
  "$MODDIR/agent" -port 5556 >> /data/local/devctl/agent.log 2>&1
  sleep 10
done &
