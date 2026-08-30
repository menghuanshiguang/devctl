#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""devctl — 局域网设备控制发送端 CLI (LLM 友好: --json, 零交互)"""
import argparse
import base64
import json
import os
import socket
import ssl
import struct
import sys
import time

CONFIG = os.path.expanduser("~/.devctl/config.json")
PORT_DEFAULT = 5556
TOKEN_DEFAULT = "devctl"


# ---------- 配置 ----------
def cfg_load():
    try:
        with open(CONFIG, encoding="utf-8") as f:
            return json.load(f)
    except FileNotFoundError:
        c = {"devices": []}
        cfg_save(c)
        return c


def cfg_save(c):
    os.makedirs(os.path.dirname(CONFIG) or ".", exist_ok=True)
    with open(CONFIG, "w", encoding="utf-8") as f:
        json.dump(c, f, indent=2, ensure_ascii=False)


def find(name):
    for d in cfg_load()["devices"]:
        if d["name"] == name:
            return d
    sys.exit(f"设备 {name} 未配置, 先 devctl add")


# ---------- 通道 ----------
def send(s, m):
    s.sendall((json.dumps(m, ensure_ascii=False) + "\n").encode())


class LineReader:
    def __init__(self, s):
        self.s, self.buf = s, b""

    def next(self):
        while b"\n" not in self.buf:
            chunk = self.s.recv(65536)
            if not chunk:
                raise ConnectionError("连接断开")
            self.buf += chunk
        line, self.buf = self.buf.split(b"\n", 1)
        return json.loads(line)


def connect(d, timeout=8):
    last = None
    if not d.get("tls", True):
        # 明文模式 (迁移/兼容)
        for delay in (0, 0.5, 1, 2):
            try:
                s = socket.create_connection((d["host"], d.get("port", PORT_DEFAULT)), timeout=timeout)
                s.settimeout(timeout)
                send(s, {"t": "hello", "token": d.get("token", TOKEN_DEFAULT), "name": d["name"]})
                ack = LineReader(s).next()
                if not ack.get("ok"):
                    s.close()
                    sys.exit(f"鉴权失败: {ack.get('stderr', '')}")
                return s
            except (OSError, socket.timeout) as e:
                last = e
                time.sleep(delay)
        sys.exit(f"连接 {d['host']}:{d.get('port', PORT_DEFAULT)} 失败: {last}")
    cert_dir = os.path.expanduser("~/.devctl/certs")
    os.makedirs(cert_dir, exist_ok=True)
    cert_file = os.path.join(cert_dir, d["name"] + ".pem")
    for delay in (0, 0.5, 1, 2):  # 自动重试 4 次
        try:
            raw = socket.create_connection((d["host"], d.get("port", PORT_DEFAULT)), timeout=timeout)
            if os.path.exists(cert_file):
                # 已有证书: 严格校验 (TOFU)
                ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                ctx.load_verify_locations(cert_file)
                ctx.check_hostname = False
                ctx.verify_mode = ssl.CERT_REQUIRED
                s = ctx.wrap_socket(raw, server_hostname=d["host"])
            else:
                # 首次连接: 获取证书保存 (Trust On First Use)
                ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                ctx.check_hostname = False
                ctx.verify_mode = ssl.CERT_NONE
                probe = ctx.wrap_socket(raw, server_hostname=d["host"])
                der = probe.getpeercert(binary_form=True)
                if der:
                    with open(cert_file, "w") as f:
                        f.write(ssl.DER_cert_to_PEM_cert(der))
                probe.close()
                raw = socket.create_connection((d["host"], d.get("port", PORT_DEFAULT)), timeout=timeout)
                ctx2 = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
                ctx2.load_verify_locations(cert_file)
                ctx2.check_hostname = False
                ctx2.verify_mode = ssl.CERT_REQUIRED
                s = ctx2.wrap_socket(raw, server_hostname=d["host"])
            s.settimeout(timeout)
            send(s, {"t": "hello", "token": d.get("token", TOKEN_DEFAULT), "name": d["name"]})
            ack = LineReader(s).next()
            if not ack.get("ok"):
                s.close()
                sys.exit(f"鉴权失败: {ack.get('stderr', '')}")
            return s
        except (OSError, socket.timeout) as e:
            last = e
            time.sleep(delay)
    sys.exit(f"连接 {d['host']}:{d.get('port', PORT_DEFAULT)} 失败: {last}")


def cmd_call(d, method, args, data="", timeout=30):
    s = connect(d)
    s.settimeout(timeout)
    send(s, {"t": "cmd", "id": 1, "method": method, "args": args, "data": data})
    try:
        return LineReader(s).next()
    finally:
        s.close()


def emit(a, r):
    if a.json:
        print(json.dumps({
            "ok": bool(r.get("ok")), "rc": r.get("rc", 0),
            "stdout": r.get("stdout", ""), "stderr": r.get("stderr", ""),
            "data": r.get("data", ""),
        }, ensure_ascii=False))
    else:
        if r.get("stdout"):
            print(r["stdout"], end="" if r["stdout"].endswith("\n") else "\n")
        if r.get("stderr"):
            sys.stderr.write(r["stderr"])
        if r.get("data", "").startswith(("[", "{")):
            try:
                print(json.dumps(json.loads(r["data"]), ensure_ascii=False, indent=2))
            except ValueError:
                pass
    sys.exit(0 if r.get("ok") else 1)


# ---------- 子命令 ----------
def cmd_devices(a):
    devs = cfg_load()["devices"]
    if a.json:
        print(json.dumps({"devices": devs}, ensure_ascii=False))
    elif not devs:
        print("(无设备) 用 devctl add 添加")
    else:
        for d in devs:
            print(f"{d['name']:<14} {d.get('type', '?'):<9} {d['host']}:{d.get('port', PORT_DEFAULT)}")
    sys.exit(0)


def cmd_add(a):
    c = cfg_load()
    if any(x["name"] == a.name for x in c["devices"]):
        sys.exit(f"设备 {a.name} 已存在")
    d = {"name": a.name, "type": a.type, "host": a.host}
    if a.port:
        d["port"] = a.port
    if a.token:
        d["token"] = a.token
    if getattr(a, "no_tls", False):
        d["tls"] = False
    c["devices"].append(d)
    cfg_save(c)
    print(f"已添加 {a.name}: {a.type} @ {a.host}:{a.port or PORT_DEFAULT}")


def cmd_rm(a):
    c = cfg_load()
    c["devices"] = [x for x in c["devices"] if x["name"] != a.name]
    cfg_save(c)
    print(f"已删除 {a.name}")


def cmd_ping(a):
    d = find(a.name)
    ok = True
    try:
        s = connect(d, timeout=5)
        send(s, {"t": "ping"})
        LineReader(s).next()
    except (OSError, socket.timeout, ConnectionError, SystemExit) as e:
        ok = False
        err = str(e)
        if err and not a.json:
            sys.stderr.write(err + "\n")
    if a.json:
        print(json.dumps({"ok": ok, "device": a.name}, ensure_ascii=False))
    else:
        print(f"{a.name}: {'OK' if ok else 'FAIL'}")
    sys.exit(0 if ok else 1)


def cmd_run(a):
    d = find(a.name)
    if a.method == "push":
        if len(a.args) < 2:
            sys.exit("usage: run <name> push <local> <remote>")
        try:
            b = open(a.args[0], "rb").read()
        except OSError as e:
            sys.exit(f"读取 {a.args[0]} 失败: {e}")
        r = cmd_call(d, "push", [a.args[1]], data=base64.b64encode(b).decode(), timeout=a.timeout)
        emit(a, r)
    if a.method == "pull":
        if len(a.args) < 2:
            sys.exit("usage: run <name> pull <remote> <local>")
        r = cmd_call(d, "pull", [a.args[0]], timeout=a.timeout)
        if r.get("ok") and r.get("data"):
            with open(a.args[1], "wb") as f:
                f.write(base64.b64decode(r["data"]))
        emit(a, r)
    r = cmd_call(d, a.method, a.args, timeout=a.timeout)
    emit(a, r)


def cmd_logcat(a):
    d = find(a.name)
    s = connect(d, timeout=8)
    s.settimeout(None)  # follow 模式不超时
    send(s, {"t": "cmd", "id": 1, "method": "logcat", "args": [a.filter or ""], "data": ""})
    r = LineReader(s).next()
    if not r.get("ok"):
        print(f"logcat 启动失败: {r.get('stderr', '')}")
        sys.exit(1)
    try:
        rd = LineReader(s)
        while True:
            m = rd.next()
            if m.get("t") == "evt":
                if a.json:
                    print(json.dumps(m, ensure_ascii=False), flush=True)
                else:
                    print(m.get("data", ""), flush=True)
            elif m.get("t") == "res":
                break
    except (ConnectionError, KeyboardInterrupt, OSError):
        pass  # 断开即停 (v1 简化)


def cmd_update(a):
    """热更新 agent: push → 验证 → 备份 → 替换 → pkill(service.sh 自动拉起)"""
    d = find(a.name)
    try:
        b = open(a.binary, "rb").read()
    except OSError as e:
        sys.exit(f"读取 {a.binary} 失败: {e}")
    if not b:
        sys.exit("二进制为空")
    # 1. push 新二进制
    r = cmd_call(d, "push", ["/data/local/devctl/agent.new"], data=base64.b64encode(b).decode(), timeout=a.timeout)
    if not r.get("ok"):
        emit(a, r)
    # 2. 验证可执行 + 取版本
    r = cmd_call(d, "shell", ["chmod 755 /data/local/devctl/agent.new && /data/local/devctl/agent.new -version"], timeout=a.timeout)
    if not r.get("ok"):
        emit(a, r)
    newver = r.get("stdout", "").strip()
    # 3. 备份旧版 + 原子替换 (mv rename, 运行中的文件不可 cp 覆盖)
    r = cmd_call(d, "shell", [f"cp {a.remote} /data/local/devctl/agent.bak && cp /data/local/devctl/agent.new {a.remote}.new && mv -f {a.remote}.new {a.remote} && chmod 755 {a.remote}"], timeout=a.timeout)
    if not r.get("ok"):
        emit(a, r)
    # 4. 杀旧进程, 连接会断 (预期), service.sh 10s 内拉起新版本
    try:
        cmd_call(d, "shell", [f"pkill -f '^{a.remote}' || true"], timeout=10)
    except (SystemExit, ConnectionError, OSError, socket.timeout):
        pass
    time.sleep(12)
    # 5. 验证恢复
    try:
        s = connect(d, timeout=8)
        send(s, {"t": "ping"})
        LineReader(s).next()
        s.close()
    except (OSError, socket.timeout, ConnectionError, SystemExit) as e:
        sys.exit(f"更新后连接失败: {e} (可回滚: 重新 push agent.bak)")
    print(f"更新完成: {newver} (备份: /data/local/devctl/agent.bak)")
    sys.exit(0)


def cmd_ops(a):
    """查看接收端操作记录 (ops.log)"""
    d = find(a.name)
    r = cmd_call(d, "shell", ["tail -n %d /data/local/devctl/ops.log 2>/dev/null" % a.tail], timeout=a.timeout)
    if not r.get("ok"):
        emit(a, r)
    print(r.get("stdout", ""), end="")
    sys.exit(0)


LAST = os.path.expanduser("~/.devctl/last_search.json")


def cmd_find(a):
    """搜索/过滤: find <name> <pid|pkg> <type> <value> [max] | find <name> <pid> <changed|increased|decreased>"""
    d = find(a.name)
    if a.filter:
        r = cmd_call(d, "mem_search", [a.pid, a.filter], timeout=a.timeout)
    else:
        args = [a.pid, a.type, a.value]
        if a.max:
            args.append(str(a.max))
        r = cmd_call(d, "mem_search", args, timeout=a.timeout)
    if not r.get("ok"):
        emit(a, r)
    data = json.loads(r["data"])
    with open(LAST, "w", encoding="utf-8") as f:
        json.dump({"name": a.name, "pid": a.pid, "addrs": [s["addr"] for s in data["sample"]], "count": data["count"]}, f)
    if a.json:
        print(json.dumps({"ok": True, **data}, ensure_ascii=False))
    else:
        print(f"匹配 {data['count']} 个地址")
        for s in data["sample"][:10]:
            print(f"  {s['addr']}  {s['value']}")
        if data["count"] > len(data["sample"]):
            print("  ... (完整列表已存, 可用 writeall 批量写)")
    sys.exit(0)


def cmd_writeall(a):
    """对最近一次 find 结果的所有地址批量写入"""
    try:
        last = json.load(open(LAST, encoding="utf-8"))
    except (OSError, ValueError):
        sys.exit("没有 find 结果, 先 devctl find")
    d = find(last["name"])
    pid = a.pid or last["pid"]
    ok = 0
    for addr in last["addrs"]:
        r = cmd_call(d, "mem_write", [str(pid), addr, a.type, a.value], timeout=a.timeout)
        if r.get("ok"):
            ok += 1
    print(f"写入 {ok}/{len(last['addrs'])} 个地址")
    sys.exit(0)


def cmd_fovflicker(a):
    """fov 线性渐变: 1°↔120° 平滑往返 (Ctrl-C 停止, 停止时恢复 90°)"""
    d = find(a.name)
    addr = a.addr
    if not addr:
        try:
            cfg = json.load(open(FOV_CFG, encoding="utf-8"))
            addr = cfg["addr"]
            print(f"使用已保存 fov 地址: {addr}")
        except (OSError, ValueError, KeyError):
            pass
    if not addr:
        if not a.value:
            sys.exit("需要 --addr 已知地址, 或先 devctl fovfind, 或 --value 自动定位")
        addr = locate_fov(a, d, a.value)
        with open(FOV_CFG, "w", encoding="utf-8") as f:
            json.dump({"pid": a.pid, "addr": addr}, f)
        print(f"自动定位 fov @ {addr}")
    lo, hi = a.lo, a.hi
    print(f"fov 线性渐变 {lo}°↔{hi}° @ {addr} 步进 {a.step}°/{a.interval*1000:.0f}ms (Ctrl-C 停止)")
    try:
        v, delta = float(lo), float(a.step)
        while True:
            cmd_call(d, "mem_write", [a.pid, addr, "f32", f"{v:.4f}"], timeout=30)
            time.sleep(a.interval)
            v += delta
            if v >= hi:
                v, delta = float(hi), -abs(a.step)
            elif v <= lo:
                v, delta = float(lo), abs(a.step)
    except KeyboardInterrupt:
        cmd_call(d, "mem_write", [a.pid, addr, "f32", "90"], timeout=30)
        print("\n已停止, fov 恢复 90°")


FOV_CFG = os.path.expanduser("~/.devctl/fov.json")


def locate_fov(a, d, cur_value):
    """交互定位 fov: 搜当前度数 → 用户调+1 → changed → 找精确新值"""
    r = cmd_call(d, "mem_search", [a.pid, "f32", str(cur_value), "50000"], timeout=600)
    if not r.get("ok"):
        emit(a, r)
    n = json.loads(r["data"])["count"]
    print(f"搜到 {n} 个, 请把视角调到 {cur_value + 1} 度, 完成后回车...")
    input()
    r = cmd_call(d, "mem_search", [a.pid, "changed"], timeout=600)
    if not r.get("ok"):
        emit(a, r)
    data = json.loads(r["data"])
    target = struct.pack("<f", cur_value + 1).hex()
    cand = [s for s in data["sample"] if s["value"] == target]
    if not cand:
        print("changed 结果:", [(s["addr"], s["value"]) for s in data["sample"][:10]])
        sys.exit("定位失败: changed 里没有精确新值 (确认视角已改变)")
    return cand[0]["addr"]


def cmd_fovfind(a):
    """一键定位 fov 地址并保存 (游戏重启后重新定位)"""
    d = find(a.name)
    if a.value is None:
        sys.exit("usage: fovfind <name> <pid> --value <当前视角度数>")
    addr = locate_fov(a, d, a.value)
    with open(FOV_CFG, "w", encoding="utf-8") as f:
        json.dump({"pid": a.pid, "addr": addr}, f)
    print(f"✅ fov 地址: {addr} (已保存, fovflicker 自动使用)")
    sys.exit(0)


def cmd_fovlock(a):
    """代码 patch: 锁定视角为指定度数 (默认 120), 重启后需重新锁定"""
    d = find(a.name)
    deg = a.deg or 120
    bits = struct.unpack("<I", struct.pack("<f", float(deg)))[0]
    if bits & 0xFFFF:
        sys.exit("仅支持整数度数")
    hex4 = (0x52800000 | (1 << 21) | ((bits >> 16) << 5) | 8).to_bytes(4, "little").hex()
    r = cmd_call(d, "mem_patchlib", [a.pid, "libminecraftpe.so", "0x118bdb48", hex4], timeout=30)
    if r.get("ok"):
        print(f"✅ 视角已锁定 {deg}° (patch: ldr->mov {hex4})")
        sys.exit(0)
    emit(a, r)


def cmd_fovunlock(a):
    """恢复原指令 (视角回设置值)"""
    d = find(a.name)
    r = cmd_call(d, "mem_patchlib", [a.pid, "libminecraftpe.so", "0x118bdb48", "08e84abd"], timeout=30)
    if r.get("ok"):
        print("✅ 已恢复 (视角回设置值)")
        sys.exit(0)
    emit(a, r)


CUR_FILE = os.path.expanduser("~/.devctl/cur.json")


def cmd_cur(a):
    """获取并保存当前前台应用 (供后续命令直接使用)"""
    d = find(a.name)
    r = cmd_call(d, "shell", ["dumpsys activity activities | grep -E 'topResumedActivity=' | head -1"], timeout=30)
    if not r.get("ok"):
        emit(a, r)
    line = r.get("stdout", "")
    import re
    m = re.search(r"topResumedActivity=ActivityRecord\{[^ ]+ u0 ([^/\s]+)", line)
    if not m:
        sys.exit(f"解析失败: {line.strip()[:80]}")
    pkg = m.group(1)
    r = cmd_call(d, "mem_pid", [pkg], timeout=30)
    pid = r.get("stdout", "").strip()
    with open(CUR_FILE, "w", encoding="utf-8") as f:
        json.dump({"pkg": pkg, "pid": pid, "time": time.strftime("%H:%M:%S")}, f)
    print(f"当前应用: {pkg} (pid {pid}) 已保存")
    sys.exit(0)


def cur_target(a):
    """返回 (pkg, pid): 优先 --pid, 否则读保存的当前应用"""
    if a.pid:
        return None, a.pid
    try:
        c = json.load(open(CUR_FILE, encoding="utf-8"))
        return c["pkg"], c["pid"]
    except (OSError, ValueError, KeyError):
        sys.exit("没有保存的当前应用, 先 devctl cur")


def cmd_play(a):
    """播放音频: 转wav → 推送 → 关DND → 系统播放器播放 (备选tinyplay直出)"""
    d = find(a.name)
    import subprocess
    tmp = "/tmp/devctl_play48.wav"
    r = subprocess.run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
                        "-i", a.file, "-ar", "48000", "-ac", "2", tmp],
                       capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"ffmpeg 转换失败: {r.stderr[:200]}")
    try:
        b = open(tmp, "rb").read()
    except OSError as e:
        sys.exit(f"读取失败: {e}")
    r = cmd_call(d, "push", ["/data/local/tmp/devctl_play.wav"], data=base64.b64encode(b).decode(), timeout=120)
    if not r.get("ok"):
        emit(a, r)
    # 关闭勿扰 (DND 会导致系统/媒体静音, 踩坑记录)
    cmd_call(d, "shell", ["cmd notification set_dnd off 2>/dev/null || true"], timeout=30)
    # 系统播放器播放 (AudioFlinger 自动配置路由/功放)
    r = cmd_call(d, "shell", ["am start -a android.intent.action.VIEW -d file:///data/local/tmp/devctl_play.wav -t audio/x-wav 2>&1"], timeout=30)
    if not r.get("ok"):
        emit(a, r)
    print(f"🎵 正在手机播放: {a.file}")
    sys.exit(0)


def cmd_volume(a):
    """设置媒体音量百分比 (0-100)"""
    d = find(a.name)
    r = cmd_call(d, "shell", ["dumpsys audio | grep -A6 'STREAM_MUSIC:' | grep Max | head -1"], timeout=30)
    try:
        maxv = int(r.get("stdout", "").split(":")[1].strip())
    except (ValueError, IndexError):
        maxv = 160
    level = max(0, min(100, a.percent)) * maxv // 100
    r = cmd_call(d, "shell", [f"cmd media_session volume --stream 3 --set {level}"], timeout=30)
    if r.get("ok"):
        print(f"🔊 媒体音量: {a.percent}% ({level}/{maxv})")
        sys.exit(0)
    emit(a, r)


def cmd_bt(a):
    """蓝牙开关"""
    d = find(a.name)
    if a.action == "status":
        r = cmd_call(d, "shell", ["settings get global bluetooth_on 2>/dev/null || echo 未知"], timeout=30)
        print(f"蓝牙: {'开启' if r.get('stdout','').strip() == '1' else '关闭'}")
        sys.exit(0)
    cmd = "enable" if a.action == "on" else "disable"
    r = cmd_call(d, "shell", [f"svc bluetooth {cmd} 2>&1 | tail -1"], timeout=30)
    if r.get("ok") and "Success" in r.get("stdout", ""):
        print(f"✅ 蓝牙已{'开启' if a.action == 'on' else '关闭'}")
        sys.exit(0)
    emit(a, r)


def cmd_wifi(a):
    """WiFi 开关"""
    d = find(a.name)
    if a.action == "status":
        r = cmd_call(d, "shell", ["settings get global wifi_on 2>/dev/null || echo 未知"], timeout=30)
        print(f"WiFi: {'开启' if r.get('stdout','').strip() == '1' else '关闭'}")
        sys.exit(0)
    cmd = "enable" if a.action == "on" else "disable"
    r = cmd_call(d, "shell", [f"svc wifi {cmd} 2>&1 | tail -1"], timeout=30)
    if r.get("ok") and "Success" in r.get("stdout", ""):
        print(f"✅ WiFi 已{'开启' if a.action == 'on' else '关闭'}")
        sys.exit(0)
    emit(a, r)


def cmd_unlock(a):
    """解锁手机: 唤醒→上滑→密码 (自动处理通知栏/systemui 卡死)"""
    d = find(a.name)
    pwd = a.password
    if not pwd:
        try:
            pwd = open(os.path.expanduser("~/.devctl/unlock_pwd.txt"), encoding="utf-8").read().strip()
        except (OSError, ValueError):
            sys.exit("需要 --password 参数, 或写 ~/.devctl/unlock_pwd.txt")
    # 1. 确保屏幕亮
    r = cmd_call(d, "shell", ["dumpsys power | grep mWakefulness | head -1"], timeout=30)
    if "Awake" not in r.get("stdout", ""):
        cmd_call(d, "shell", ["input keyevent 224"], timeout=30)
        time.sleep(2)
    # 2. 已解锁?
    r = cmd_call(d, "shell", ["dumpsys window | grep mDreamingLockscreen | head -1"], timeout=30)
    if "true" not in r.get("stdout", ""):
        print("✅ 手机未锁屏")
        sys.exit(0)
    # 3. 解锁: 序列 (224→swipe→text→66), 失败后重启 systemui 清状态再试
    for attempt in range(a.retry):
        if attempt > 0:
            cmd_call(d, "shell", ["pkill -f com.android.systemui || true"], timeout=30)
            time.sleep(9)
        cmd_call(d, "shell", ["input keyevent 224; sleep 2; input swipe 600 2500 600 900 300; sleep 2; "
                              f"input text {pwd}; sleep 1; input keyevent 66"], timeout=60)
        time.sleep(3)
        r = cmd_call(d, "shell", ["dumpsys window | grep -E 'mDreamingLockscreen|isKeyguardShowing' | head -2"], timeout=30)
        out = r.get("stdout", "")
        if "false" in out and "true" not in out:
            print(f"✅ 解锁成功 (第 {attempt + 1} 轮)")
            sys.exit(0)
    sys.exit(f"解锁失败: {a.retry} 轮后仍锁屏")


def cmd_screen(a):
    """亮屏/息屏/状态"""
    d = find(a.name)
    if a.action == "on":
        cmd_call(d, "shell", ["input keyevent 224"], timeout=30)
        print("✅ 已亮屏")
    elif a.action == "off":
        cmd_call(d, "shell", ["input keyevent 26"], timeout=30)
        print("✅ 已息屏")
    else:
        r = cmd_call(d, "shell", ["dumpsys power | grep mWakefulness | head -1"], timeout=30)
        print(r.get("stdout", "").strip())
    sys.exit(0)


def cmd_hide(a):
    """隐藏 agent 进程"""
    d = find(a.name)
    args = [a.disguise] if a.disguise else []
    r = cmd_call(d, "hide_start", args, timeout=a.timeout)
    emit(a, r)


def cmd_unhide(a):
    """停止隐藏"""
    d = find(a.name)
    r = cmd_call(d, "hide_stop", [], timeout=a.timeout)
    emit(a, r)


def main():
    j = argparse.ArgumentParser(add_help=False)
    j.add_argument("--json", action="store_true", help="结构化 JSON 输出")
    p = argparse.ArgumentParser(prog="devctl", description="局域网设备控制发送端 (LLM 友好)")
    sub = p.add_subparsers(dest="cmd", required=True)

    sp = sub.add_parser("devices", parents=[j], help="列出设备")
    sp.set_defaults(fn=cmd_devices)

    sp = sub.add_parser("add", parents=[j], help="添加设备")
    sp.add_argument("name")
    sp.add_argument("--type", required=True, choices=["android", "windows"])
    sp.add_argument("--host", required=True)
    sp.add_argument("--port", type=int)
    sp.add_argument("--token", help=f"鉴权 token (默认 {TOKEN_DEFAULT})")
    sp.add_argument("--no-tls", action="store_true", help="明文模式 (旧版 agent 迁移用)")
    sp.set_defaults(fn=cmd_add)

    sp = sub.add_parser("rm", parents=[j], help="删除设备")
    sp.add_argument("name")
    sp.set_defaults(fn=cmd_rm)

    sp = sub.add_parser("ping", parents=[j], help="连通性测试")
    sp.add_argument("name")
    sp.set_defaults(fn=cmd_ping)

    sp = sub.add_parser("run", parents=[j], help="调用 agent 方法")
    sp.add_argument("name")
    sp.add_argument("method")
    sp.add_argument("args", nargs="*")
    sp.add_argument("--timeout", type=int, default=30)
    sp.set_defaults(fn=cmd_run)

    sp = sub.add_parser("logcat", parents=[j], help="实时日志 (流式, Ctrl-C 停止)")
    sp.add_argument("name")
    sp.add_argument("--filter", default="")
    sp.set_defaults(fn=cmd_logcat)

    sp = sub.add_parser("update", parents=[j], help="热更新 agent 二进制 (无需重启/刷模块)")
    sp.add_argument("name")
    sp.add_argument("binary", help="本地新二进制路径")
    sp.add_argument("--remote", default="/data/adb/modules/devctl_agent/agent", help="agent 可执行文件路径")
    sp.add_argument("--timeout", type=int, default=60)
    sp.set_defaults(fn=cmd_update)

    sp = sub.add_parser("ops", parents=[j], help="查看接收端操作记录 (ops.log)")
    sp.add_argument("name")
    sp.add_argument("--tail", type=int, default=50)
    sp.add_argument("--timeout", type=int, default=30)
    sp.set_defaults(fn=cmd_ops)

    sp = sub.add_parser("find", parents=[j], help="内存搜索/过滤循环 (结果存 last_search, 供 writeall)")
    sp.add_argument("name")
    sp.add_argument("pid", help="进程 pid 或包名")
    sp.add_argument("--type", help="i8/i16/i32/i64/f32/f64/str/hex")
    sp.add_argument("--value")
    sp.add_argument("--filter", choices=["changed", "increased", "decreased"], help="基于上次结果过滤")
    sp.add_argument("--max", type=int, default=50000)
    sp.add_argument("--timeout", type=int, default=600)
    sp.set_defaults(fn=cmd_find)

    sp = sub.add_parser("writeall", parents=[j], help="对最近 find 结果批量写入")
    sp.add_argument("--type", required=True, help="i8/i16/i32/i64/f32/f64/str/hex")
    sp.add_argument("--value", required=True)
    sp.add_argument("--pid", help="覆盖保存的 pid")
    sp.add_argument("--timeout", type=int, default=60)
    sp.set_defaults(fn=cmd_writeall)

    sp = sub.add_parser("fovflicker", parents=[j], help="fov 线性渐变 1°↔120° 平滑往返 (Ctrl-C 停止)")
    sp.add_argument("name")
    sp.add_argument("pid", help="进程 pid 或包名")
    sp.add_argument("--addr", help="已知 fov 地址 (跳过定位)")
    sp.add_argument("--value", type=float, help="当前视角度数 (自动定位用)")
    sp.add_argument("--lo", type=float, default=1, help="最低视角 (默认 1)")
    sp.add_argument("--hi", type=float, default=120, help="最高视角 (默认 120)")
    sp.add_argument("--step", type=float, default=1, help="每步度数 (默认 1)")
    sp.add_argument("--interval", type=float, default=0.02, help="每步间隔秒 (默认 0.02)")
    sp.set_defaults(fn=cmd_fovflicker)

    sp = sub.add_parser("fovfind", parents=[j], help="一键定位 fov 地址并保存")
    sp.add_argument("name")
    sp.add_argument("pid", help="进程 pid 或包名")
    sp.add_argument("--value", type=float, required=True, help="当前视角度数")
    sp.set_defaults(fn=cmd_fovfind)

    sp = sub.add_parser("fovlock", parents=[j], help="代码 patch 锁定视角 (重启后重新锁定)")
    sp.add_argument("name")
    sp.add_argument("pid", help="进程 pid 或包名")
    sp.add_argument("--deg", type=int, help="锁定度数 (默认 120)")
    sp.set_defaults(fn=cmd_fovlock)

    sp = sub.add_parser("fovunlock", parents=[j], help="恢复视角代码")
    sp.add_argument("name")
    sp.add_argument("pid", help="进程 pid 或包名")
    sp.set_defaults(fn=cmd_fovunlock)

    sp = sub.add_parser("cur", parents=[j], help="获取并保存当前前台应用")
    sp.add_argument("name")
    sp.set_defaults(fn=cmd_cur)

    sp = sub.add_parser("play", parents=[j], help="播放音频到设备 (自动转48k+关DND+系统播放器)")
    sp.add_argument("name")
    sp.add_argument("file", help="本地音频文件 (mp3/wav/m4a 等)")
    sp.set_defaults(fn=cmd_play)

    sp = sub.add_parser("volume", parents=[j], help="设置媒体音量百分比")
    sp.add_argument("name")
    sp.add_argument("percent", type=int, help="0-100")
    sp.set_defaults(fn=cmd_volume)

    for name, fn, desc in (("bt", cmd_bt, "蓝牙开关"), ("wifi", cmd_wifi, "WiFi 开关")):
        sp = sub.add_parser(name, parents=[j], help=desc)
        sp.add_argument("name")
        sp.add_argument("action", choices=["on", "off", "status"], help="on/off/status")
        sp.set_defaults(fn=fn)

    sp = sub.add_parser("unlock", parents=[j], help="解锁手机 (混沌系统, 多轮变招重试)")
    sp.add_argument("name")
    sp.add_argument("--password", help="锁屏密码 (缺省读 ~/.devctl/unlock_pwd.txt)")
    sp.add_argument("--retry", type=int, default=4, help="尝试轮数 (默认 4)")
    sp.set_defaults(fn=cmd_unlock)

    sp = sub.add_parser("screen", parents=[j], help="亮屏/息屏/状态")
    sp.add_argument("name")
    sp.add_argument("action", choices=["on", "off", "status"])
    sp.set_defaults(fn=cmd_screen)

    sp = sub.add_parser("hide", parents=[j], help="隐藏 agent 进程 (prctl改名 + ptrace hook /proc)")
    sp.add_argument("name")
    sp.add_argument("--name-disguise", dest="disguise", help="伪装进程名 (默认 kworker/u16:2)")
    sp.add_argument("--timeout", type=int, default=30)
    sp.set_defaults(fn=cmd_hide)

    sp = sub.add_parser("unhide", parents=[j], help="停止隐藏, 恢复原进程名")
    sp.add_argument("name")
    sp.add_argument("--timeout", type=int, default=30)
    sp.set_defaults(fn=cmd_unhide)

    a = p.parse_args()
    a.fn(a)


if __name__ == "__main__":
    main()
