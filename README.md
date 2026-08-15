# devctl — 局域网设备控制

发送端 CLI（大模型调用）+ 接收端 agent（root 常驻）之间的一条 TCP 长连接通道。
目标设备：root 的 Android（Magisk 模块）／Windows（M3 待开发）。

```
LLM (Minis) → devctl CLI (发送端) → TCP:5556 ← agent (接收端, root daemon)
```

## 控制端（iSH）

```sh
apk add android-tools   # 不需要, 只用 python3 (自带)
python3 client/devctl.py add phone --type android --host <手机IP> --token devctl
python3 client/devctl.py ping phone
python3 client/devctl.py run phone shell "id"          # root 命令
python3 client/devctl.py run phone apps                # 第三方应用列表
python3 client/devctl.py run phone install /data/local/tmp/app.apk
python3 client/devctl.py run phone extract com.tencent.mm /data/local/devctl/out
python3 client/devctl.py run phone push ./a.apk /data/local/tmp/a.apk
python3 client/devctl.py run phone pull /data/local/devctl/agent.log ./agent.log
python3 client/devctl.py logcat phone --filter AndroidRuntime:E  # Ctrl-C 停止
```

所有命令加 `--json` 输出结构化结果（LLM 友好）。

## Windows 接入（接收器）

```powershell
# 1. 拷贝 agent-windows.exe 到目标机 (如 C:\devctl\agent.exe)
# 2. 注册为服务 (开机自启 + 失败自动重启)
sc create devctl-agent binPath= "C:\devctl\agent.exe" start= auto
sc failure devctl-agent reset= 60 actions= restart/5000/restart/10000/restart/30000
sc start devctl-agent
# 3. 防火墙放行 (默认端口 5556)
netsh advfirewall firewall add rule name="devctl" dir=in action=allow protocol=TCP localport=5556
# 4. 控制端连接
devctl add pc --type windows --host <PC-IP>
devctl ping pc
devctl run pc shell "echo hello"
devctl run pc ps        # 进程列表
```

- 也可直接前台运行 `agent.exe -port 5556`（调试）
- Windows 方法：shell（cmd /c）、sysinfo、ps、push/pull、ping
- 构建：`cd agent && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -p 1 -ldflags "-s -w" -o ../release/agent-windows.exe .`

## 安卓接入（Magisk）

1. 下载 `devctl-android-v0.1.zip` → Magisk App → 模块 → 从本地安装 → 重启
2. 验证 agent 在跑（终端或 adb shell）：
   ```sh
   ps -A | grep agent          # 应看到 /data/adb/modules/devctl_agent/agent
   cat /data/local/devctl/agent.log   # 应有 "devctl-agent v0.1 listening on :5556"
   ```
3. 控制端：`devctl add phone --type android --host <手机IP>`
4. 改 token：编辑 `/data/adb/modules/devctl_agent/service.sh` 中 `-token` 参数（控制端 `add --token` 同步改）

## 实机测试清单（刷机后）

| # | 操作 | 预期 |
|---|---|---|
| 1 | 重启后 `ps -A \| grep agent` | 进程存在 |
| 2 | `cat /data/local/devctl/agent.log` | `listening on :5556` |
| 3 | 控制端 `devctl ping phone` | OK |
| 4 | `devctl run phone shell "id"` | `uid=0(root)` |
| 5 | `devctl run phone apps` | 应用列表 |
| 6 | push 一个 apk 后 `install` | 安装成功 |
| 7 | `extract` 一个已装应用 | 输出 apk 文件路径 |
| 8 | `logcat --filter ActivityManager:I` | 实时滚动日志 |
| 9 | 杀 agent 进程（`kill <pid>`） | 10 秒后自动拉起，ping 恢复 |

## 安全边界（v1）

- 通道为局域网明文 + token 鉴权（默认 `devctl`）。**禁止把 5556 端口映射到公网**
- agent 以 root 常驻 = 拿到 token 的人等于拿到设备，只连可信 Wi-Fi
- 建议路由器开 AP 隔离 / 访客网络放不信任设备

## 协议

JSON Lines over TCP：`hello`(鉴权) / `cmd` / `res` / `evt`(logcat 流) / `ping` / `pong`。
方法：shell、apps、install、extract、push、pull、logcat、sysinfo。详见 DESIGN.md。

## 构建

```sh
# Android (arm64)
cd agent && CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -ldflags "-s -w" -o ../release/agent .
# 打包模块
cd magisk && cp ../release/agent ./agent && python3 -m zipfile -c ../release/devctl-android.zip module.prop service.sh agent
```

打 tag `v0.1` 触发 GitHub Actions 自动构建 release。
