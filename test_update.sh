#!/bin/sh
# devctl 热更新流程本地测试: 模拟 Magisk service.sh 保活环境
set -e
cd /var/minis/workspace/devctl
pkill -f dagent 2>/dev/null || true
sleep 1

echo "== 编 v0.1 (旧版, 从 git tag 取源码) =="
rm -rf /tmp/v01 && mkdir -p /tmp/v01
git archive v0.1 agent | tar -x -C /tmp/v01
cd /tmp/v01/agent && go build -p 1 -o /tmp/dagent_old . && cd /var/minis/workspace/devctl

echo "== 编 v0.2 (新版) =="
go build -C agent -p 1 -o /tmp/dagent_new .

echo "== 模拟 service.sh 保活 =="
cat > /tmp/keepalive.sh <<'EOF'
#!/bin/sh
while true; do /tmp/dagent_old -port 5556 >> /tmp/dagent.log 2>&1; sleep 2; done
EOF
chmod +x /tmp/keepalive.sh
nohup /tmp/keepalive.sh > /dev/null 2>&1 &
sleep 1

echo "== 确认旧版在跑 =="
python3 client/devctl.py run local sysinfo | grep goos

echo "== 执行热更新 =="
python3 client/devctl.py update local /tmp/dagent_new --remote /tmp/dagent_old

echo "== 确认新版在跑 =="
python3 client/devctl.py run local sysinfo | grep version
python3 client/devctl.py run local shell "ls -la /tmp/dagent_old /tmp/dagent.bak 2>/dev/null"
echo "UPDATE TEST DONE"
