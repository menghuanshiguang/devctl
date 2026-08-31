# devctl 项目功能总览

版本: v0.5.3 (2026-08-31) | 架构: 控制端 CLI (iSH/任意 Python) ↔ TCP:5556 TLS ↔ agent (root daemon)

## 一、架构

```
LLM (Minis) → devctl CLI (发送端, Python 单文件) → TCP:5556 (TLS+TOFU) ← agent (接收端, root 常驻)
                                                        │
                                                        ├─ Android (Magisk 模块, 伪装 netd)
                                                        └─ Windows (服务注册, 开机自启)
```

## 二、控制端命令 (client/devctl.py)

### 设备管理
| 命令 | 说明 |
|---|---|
| `devices` | 列出设备 |
| `add` | 添加设备 (`--host` 支持 IPv4/IPv6) |
| `rm` | 删除设备 |
| `ping` | 连通性测试 |

### 远程操作
| 命令 | 说明 |
|---|---|
| `run <d> <method> [args]` | 任意 agent 方法 (shell/apps/ps/sysinfo/push/pull...) |
| `logcat` | 实时日志流 |
| `update` | 热更新 agent 二进制 (10s 自动拉起, 有备份) |
| `ops` | 操作审计日志 |

### 设备控制
| 命令 | 说明 |
|---|---|
| `cur` | 当前前台应用 |
| `unlock` | 解锁手机 (混沌重试) |
| `screen` | 亮屏/息屏/状态 |
| `play` | 播放音频到设备 (转 48k + 关 DND) |
| `volume` | 媒体音量 |
| `bt` / `wifi` | 蓝牙/WiFi 开关 |

### 内存操作 (游戏修改)
| 命令 | 说明 |
|---|---|
| `find` | 内存搜索/过滤循环 (i8/i16/.../f32/f64/str/hex) |
| `writeall` | 批量写 find 结果 |
| `fovfind/fovlock/fovunlock/fovflicker` | 视角修改工具集 |
| - | (agent: mem_read/mem_write/mem_search/mem_refs/mem_patch) |

### 进程管理
| 命令 | 说明 |
|---|---|
| `hide` | 隐藏 agent 进程 (prctl 改名 + hook /proc) |
| `unhide` | 恢复 |
| `stop` | 停止 agent |

### ⚠️ 悬浮窗 (devui) — 已取消正式功能
- `devui <d> {push,start,stop,status,open,close,refresh}` 保留命令
- 但**不作为正式功能维护** — 用 `peers` 命令替代 "看谁连接"

## 三、agent 方法 (接收端)

### 基础
- `sysinfo` / `ps` / `shell` / `apps` / `install` / `extract`
- `push` / `pull` (文件传输 ≤50MB base64)
- `logcat` / `exit`

### 内存 (游戏修改)
- `mem_read` / `mem_write` / `mem_search` (含 ?? 通配+all/code 区域)
- `mem_refs` / `mem_patch` (库) / `mem_pid`
- `hook` (ptrace 文件检测过滤)

### 进程伪装/防护
- `hide_start` / `hide_stop` / `hide_status` (伪装 netd)
- `ui_hide` / `ui_show` / `ui_status` (UI 层显示/隐藏)

### 连接状态 (v0.5.3 新增)
- `peers` — 返回当前活跃客户端列表
- dash.json — agent 自动维护 `/data/local/devctl/dash.json`
  (客户端名/地址/连接时间/最后命令, 设备端 cat 可查)

### 高级 (实验性)
- `zapi_attach` / `zapi_status` — 注入 SystemUI 的备用方案
- `scrcpy_bridge` / `vscreen_start` — 屏幕转发
- `devui_start` / `devui_stop` / `devui_status` — 悬浮窗生命周期 (保留)

## 四、关键特性

| 特性 | 说明 |
|---|---|
| 安全传输 | TLS + TOFU 证书 (首次信任, 之后严格校验) |
| 进程伪装 | agent 伪装 netd (comm/cmdline), 只操自身 |
| 自愈 | service.sh 10s 拉起崩溃 agent |
| 热更新 | update 无需重启设备, 备份回滚 |
| IPv6 兼容 | host 支持 IPv6 字面量 (+ 纯 IPv6 热点场景) |
| 横屏适配 | UI 层 (虽然不再主用) 已适配旋转 |
| 操作审计 | ops.log 每命令记录 |

## 五、安全红线 (开发纪律)

1. **绝不写系统进程内存** (system_server/zygote/surfaceflinger/systemui 只读)
2. **绝不改系统级配置** (Magisk 设置/DenyList/setprop/SELinux 等, 必须先征求同意)
3. **内存写前先读原值** (写后验证, 保留回滚)
4. **等长替换** (字符串/数值必须字节一致)
5. **目标进程限定** (只操作用户指定 app)
6. **hook/patch 自动到期脱离** (时限设置, 不留驻留)
7. **识别 agent 唯一方式** = readlink /proc/*/exe == 模块路径 (伪装不改 exe)
