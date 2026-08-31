# devctl 发送端 (客户端)

零依赖 Python3 单文件 CLI，任何装了 Python3.8+ 的机器 (Windows / Linux / macOS / iSH) 都能跑。

## 快速上手

```sh
# 1. 添加目标设备 (安卓接收端默认端口 5556)
python3 devctl.py add phone --type android --host <手机IP> --token devctl

# 1b. IPv6-only 网络 (如 iPhone 热点纯 IPv6 下发) — host 直接填全局 IPv6 地址
python3 devctl.py add phone --type android --host "2409:xxxx:xxxx::1" --token devctl

# 2. 测连通
python3 devctl.py ping phone

# 3. 常用操作
python3 devctl.py run phone shell "id"            # root 命令
python3 devctl.py run phone apps                  # 应用列表
python3 devctl.py run phone push ./a.apk /data/local/tmp/a.apk   # 推文件
python3 devctl.py run phone pull /data/local/devctl/agent.log ./agent.log
python3 devctl.py logcat phone --filter AndroidRuntime:E
python3 devctl.py cur phone                       # 取当前前台应用
python3 devctl.py unlock phone                    # 解锁手机
```

Windows 上也可以用 `devctl.bat` (同一目录下直接 `devctl add ...`)。

所有命令支持 `--json` 输出结构化结果 (LLM 友好)。

## 设备类型

- `--type android`: 接收端为 Magisk 模块里的 agent (root daemon)
- `--type windows`: 接收端为 devctl-agent 服务 (见另附的 Windows 接收端包)
- `--type mac`: 接收端为 macOS agent (待支持)

## 配置

配置保存在 `~/.devctl/config.json`，`add`/`rm` 命令维护设备清单：

```sh
python3 devctl.py devices                 # 列出设备
python3 devctl.py rm phone                # 删除设备
```

## IPv6 支持

- 接收端 agent 监听 `:5556` 为双栈 (IPv4 + IPv6)，无需额外配置
- 发送端 `--host` 直接填 IPv6 字面量即可 (TLS 握手自动跳过 SNI)
- 注意: 手机 IPv6 地址会因网络续约变化, 变后重新 `add` 或改 config.json

## 常见问题

- **连接被拒**: 确认接收端 agent 在跑 (`ps -A | grep agent` / 服务状态)，防火墙放行 5556
- **token 不匹配**: 控制端 `add --token` 需与接收端 `-token` 参数一致
- **网络变了**: 手机换了 WiFi/热点后 IP 会变，重新 `add` 或改 config.json
- **IPv6-only 热点**: iPhone 等热点在纯 IPv6 出网时不下发 IPv4, 用全局 IPv6 直连 (见上)
