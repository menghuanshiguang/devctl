#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""devctl — 局域网设备控制发送端 CLI (LLM 友好: --json, 零交互)"""
import argparse
import base64
import json
import os
import socket
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
    for delay in (0, 0.5, 1, 2):  # 自动重试 4 次
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

    a = p.parse_args()
    a.fn(a)


if __name__ == "__main__":
    main()
