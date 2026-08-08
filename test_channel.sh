#!/bin/sh
# devctl M1 通道本地测试 (iSH, 无需真机)
set -e
cd /var/minis/workspace/devctl
echo "== 编译 agent (linux arm64) =="
go build -C agent -p 1 -o /tmp/dagent .

echo "== 启动 agent =="
nohup /tmp/dagent -port 5556 > /tmp/dagent.log 2>&1 &
DPID=$!
sleep 1

echo "== 添加本地设备并 ping =="
python3 client/devctl.py rm local 2>/dev/null || true
python3 client/devctl.py add local --type android --host 127.0.0.1 --port 5556
python3 client/devctl.py ping local

echo "== shell 命令 =="
python3 client/devctl.py run local shell "echo hello-from-devctl" | grep -q hello-from-devctl && echo "PASS shell"

echo "== sysinfo =="
python3 client/devctl.py run local sysinfo --json | grep -q '"ok": true' && echo "PASS sysinfo"

echo "== push/pull 文件 =="
echo "devctl test content 你好" > /tmp/src.txt
python3 client/devctl.py run local push /tmp/src.txt /tmp/dst.txt
python3 client/devctl.py run local pull /tmp/dst.txt /tmp/back.txt
diff /tmp/src.txt /tmp/back.txt && echo "PASS push/pull"

echo "== 错误路径: 未知方法 =="
python3 client/devctl.py run local nosuchmethod 2>&1 | grep -q "unknown method" && echo "PASS unknown method"

echo "== 错误路径: 错误 token =="
python3 client/devctl.py add bad --type android --host 127.0.0.1 --port 5556 --token wrong
python3 client/devctl.py ping bad 2>&1 | grep -q "鉴权失败" && echo "PASS bad token"
python3 client/devctl.py rm bad

echo "== 断线重连: 杀 agent 后重启 =="
pkill -f dagent; sleep 1
python3 client/devctl.py ping local 2>&1 | grep -q FAIL && echo "PASS agent 被杀后 ping 失败"
nohup /tmp/dagent -port 5556 > /tmp/dagent.log 2>&1 &
sleep 1
python3 client/devctl.py ping local | grep -q OK && echo "PASS agent 重启后恢复"
pkill -f dagent

echo "ALL PASS"
