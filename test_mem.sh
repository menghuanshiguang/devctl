#!/bin/sh
# devctl 内存功能本地测试
set -e
cd /var/minis/workspace/devctl
pkill -f dagent 2>/dev/null || true
pkill -f test_mem_proc 2>/dev/null || true
sleep 1

echo "== 编译 agent =="
go build -C agent -p 1 -o /tmp/dagent .

echo "== 编译测试进程 (模拟目标应用) =="
cat > /tmp/test_mem_proc.py <<'EOF'
import ctypes, os, sys, time
arr = bytearray(16 * 1024 * 1024)
addr = ctypes.addressof(ctypes.c_char.from_buffer(arr))
val = addr + 0x123456
ctypes.c_int32.from_address(val).value = 305419896  # 0x12345678
ctypes.c_float.from_address(addr + 0x1000).value = 3.14
open("/tmp/test_mem_pid", "w").write(str(os.getpid()) + " " + hex(val) + "\n")
time.sleep(120)
EOF
nohup python3 /tmp/test_mem_proc.py > /dev/null 2>&1 &
sleep 2
PID=$(awk '{print $1}' /tmp/test_mem_pid)
VALADDR=$(awk '{print $2}' /tmp/test_mem_pid)
echo "测试进程 pid=$PID 值地址=$VALADDR"

echo "== 启动 agent =="
nohup /tmp/dagent -port 5556 > /tmp/dagent.log 2>&1 &
sleep 1
python3 client/devctl.py rm local 2>/dev/null || true
python3 client/devctl.py add local --type android --host 127.0.0.1 --port 5556

echo "== mem_pid =="
python3 client/devctl.py run local mem_pid python3 | grep -q $PID && echo "PASS mem_pid"

echo "== mem_search i32 305419896 =="
python3 client/devctl.py run local mem_search $PID i32 305419896 --json | tee /tmp/search.json
grep -q '"count": 1' /tmp/search.json && echo "PASS mem_search 精确命中"

echo "== mem_read 验证 =="
ADDR=$(python3 -c "import json;print(json.load(open('/tmp/search.json'))['data']['sample'][0]['addr'])")
python3 client/devctl.py run local mem_read $PID $ADDR 4 | grep -q 78563412 && echo "PASS mem_read (0x12345678 LE)"

echo "== mem_write 修改 =="
python3 client/devctl.py run local mem_write $PID $ADDR i32 999
python3 client/devctl.py run local mem_read $PID $ADDR 4 | grep -q e7030000 && echo "PASS mem_write (999 LE)"

echo "== changed 过滤 =="
python3 client/devctl.py run local mem_search $PID i32 999 --json | grep -q '"count": 1' && echo "PASS 重新搜索新值"
python3 -c "
import ctypes, os
addr = int('$VALADDR', 16)
" 2>/dev/null || true
# 修改测试进程内值 → changed
python3 - <<EOF
import ctypes
addr = int("$VALADDR", 16)
ctypes.c_int32.from_address(addr).value = 777
print("测试进程内已改为 777")
EOF
python3 client/devctl.py run local mem_search $PID changed --json | grep -q '"count": 1' && echo "PASS mem_search changed"
python3 client/devctl.py run local mem_read $PID $ADDR 4 | grep -q 09030000 && echo "PASS changed 后读到 777"

echo "== float 搜索 =="
python3 client/devctl.py run local mem_search $PID f32 3.14 --json | grep -q '"count": 1' && echo "PASS mem_search f32"
pkill -f test_mem_proc; pkill -f dagent
echo "ALL MEM PASS"
