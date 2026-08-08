# devctl — 局域网设备控制（开发草稿 v0.1）

> 状态：草稿待审。目标设备已 root，控制端为 iOS/iSH（LLM 直接驱动 CLI）。

## 1. 定位

在局域网内，用大模型（Minis）稳定控制一台 root 的 Android 设备 / 一台 Windows 设备：
安装应用、提取 apk、拉日志、执行任意命令、收发文件。

**核心形态**：被控端常驻 agent（接收端）+ 控制端 CLI（发送端），TCP 长连接双向通信。
不用 adb，不用 ssh，agent 自己就是 root daemon。

## 2. 架构

```
控制端 (iPhone/iSH)
  └─ devctl client (Python, LLM 调用, --json 输出)
       │  TCP 长连接 · JSON Lines · 心跳 · 自动重连
       ├─ android-agent (Go 静态二进制, Magisk 模块常驻, root)
       └─ win-agent      (Go 同款代码编译 exe, Win11 服务)
```

## 3. 协议 v1（核心规格）

- 传输：TCP，JSON Lines（每行一个 JSON 对象）
- 默认端口：`5556`（可配置）
- 连接流程：client 连上 → 发 `hello{token, name}` → agent 校验 → 回 `hello_ack{ok, version, device}` → 全双工
- 消息类型：

| 类型 | 方向 | 内容 |
|---|---|---|
| `hello` / `hello_ack` | C→A / A→C | 鉴权握手 |
| `cmd` | C→A | `{id, method, args}` |
| `res` | A→C | `{id, ok, rc, stdout, stderr, data}` |
| `evt` | A→C | `{ev, data}` 异步推送（logcat 流等） |
| `ping` / `pong` | 双向 | 心跳 |

- 心跳：client 每 30s ping；agent 90s 无消息判定断线
- 重连：client 自动重连，指数退避 1s→5s→30s 封顶，无限重试

### 方法清单（Android agent v1）

| 方法 | 说明 |
|---|---|
| `shell {cmd}` | 任意 root 命令 |
| `apps` | 第三方应用列表（pm list packages -3） |
| `install {path}` | 安装 apk（agent 本地路径，root 权限） |
| `extract {pkg, outdir}` | 提取 apk 全部 split 到 outdir，返回文件列表 |
| `push {local, remote}` / `pull {remote, local}` | 文件传输（base64 消息通道，≤50MB） |
| `logcat {filter, tail}` | 开启流式推送（evt 持续发），可随时 `logcat_stop` |
| `sysinfo` | 型号/Android 版本/内存/内网 IP |

### 方法清单（Windows agent v1）

| 方法 | 说明 |
|---|---|
| `exec {cmd}` | PowerShell 执行任意命令 |
| `push` / `pull` | 文件传输（同 base64 通道） |

## 3.5 大模型使用方式（发送端即 CLI）

发送端只有一种形态：`devctl` 命令行工具。大模型（Minis）通过 shell 直接调用：

```
devctl run phone shell "pm list packages -3" --json
devctl run phone install ./app.apk
devctl run phone logcat --follow AndroidRuntime:E
```

- 零交互（无任何 prompt），超时后自动返回
- 全局 `--json` 输出 `{ok, rc, stdout, stderr, data}`，LLM 直接解析
- 每个设备一条长连接自动管理（连不上自动重连），LLM 不需要关心连接状态

## 4. 代码结构

```
devctl/
├── agent/                  # Go，双平台一份代码
│   ├── main.go             # 入口：flag(port/token)、启动监听
│   ├── proto.go            # 消息编解码（JSON Lines）
│   ├── server.go           # TCP accept、鉴权、心跳、连接管理
│   ├── android.go          # Android 方法实现（build tag）
│   ├── windows.go          # Windows 方法实现（build tag）
│   └── files.go            # base64 文件传输
├── client/
│   └── devctl.py           # Python CLI（零依赖，纯 stdlib）
├── magisk/                 # Android 部署
│   ├── module.prop
│   ├── service.sh          # 开机启动 + 崩溃保活（while 循环）
│   └── customize.sh        # 安装时放置二进制
├── win/                    # Windows 部署
│   ├── install.ps1         # 拷贝 exe + sc create 注册服务
│   └── (devctl-agent.exe 由 CI 产出)
├── .github/workflows/build.yml   # 交叉编译 + 打包 release
└── README.md
```

## 5. 构建与部署

- **构建**：GitHub Actions，matrix 交叉编译：
  - `CGO_ENABLED=0 GOOS=android GOARCH=arm64` → Magisk 模块 zip（直接可刷）
  - `GOOS=windows GOARCH=amd64` → exe + install.ps1 zip
  - 打 tag 自动出 release 产物
- **Android 部署**：Magisk 刷模块 zip → 开机 service.sh 拉起 agent（root），while 循环保活（崩了 10s 内自动重启）
- **Windows 部署**：install.ps1 拷 exe 到 Program Files，`sc create` 注册服务（开机自启 + failure 自动重启）

## 6. 开发里程碑

| 阶段 | 内容 | 验收标准 |
|---|---|---|
| **M1 通道** | proto + agent 骨架 + client 骨架 | agent 编译成 linux/arm64 在 iSH 本机跑，loopback 联调：`run shell echo hi` 返回 ok；断连自动重连 |
| **M2 Android** | android.go 全部方法 + Magisk 模块 | 真机刷模块：install 一个 apk、extract 微信全 split、logcat 流式收 10s、shell root 命令、push/pull 文件 |
| **M3 Windows** | windows.go + install.ps1 | Win11：exec whoami、put/get 文件、开机自启 |
| **M4 加固** | 超时/错误处理/README/安全文档 | 断网 5 次重连不炸；agent 被杀自动拉起 |

## 7. 明确不做（YAGNI）

- v1 不做 TLS（token 鉴权 + 局域网明文，安全边界见 §8）
- 不做 Android app（Magisk 模块足够）
- 不做 WebSocket/REST（TCP JSON Lines 足够）
- 不做屏幕控制/截图（v2）
- 不做多设备组网/中继（点对点足够）
- 不做 32 位/旧架构（arm64 + amd64 够）

## 8. 安全边界（明文写死的约束）

- token 首次部署时随机生成，写进 agent 配置，hello 必须校验
- agent 以 root 常驻 = 局域网内拿到 token 的人等于拿到设备——**禁止把 5556 端口映射到公网**，只连可信 Wi-Fi
- 文档中明确提示：建议路由器开启 AP 隔离，不信任设备放访客网络
- 传输明文：日志/文件在局域网内可见，v2 上 TLS 解决

## 9. 待确认决策（默认值）

1. 加密：v1 token 明文（默认）／直接 TLS
2. 文件：base64 消息通道 ≤50MB（默认）／裸流快传
3. 技术栈：Go agent + Python client + GH Actions（默认）
4. 端口：5556（默认）
5. 项目名：devctl（默认）
