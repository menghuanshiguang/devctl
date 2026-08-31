# devctl ui — 悬浮控件体系 (app_process + 反射调用, 无 APK / 无注入)

## 架构

```
client devctl.py devui <...>         ← 控制端 (push/start/stop/status/open/close/refresh)
        │  (JSON Lines over TLS :5556)
agent (netd 伪装, Magisk 守护)        ← 生命周期管理 (devui_start/stop/status)
        │  setsid 拉起 + pid 文件
app_process DevctlOverlay            ← 悬浮控件进程 (comm/netd + cmdline 伪装)
        │  DevUI API 根基座
        ├─ Layer     (SurfaceControl 反射: 顶层合成, 可拖动)
        ├─ GlyphFont (devfont.bin 点阵字库: 绕开 app_process Typeface 崩溃)
        ├─ TouchInput(getevent 流: 驱动层触摸, 无需窗口系统)
        └─ CmdFile   (/data/local/tmp/devctl/cmd 轮询: 远程命令通道)
```

要点:
- **零 APK / 零注入**: 反射调用 SurfaceControl/Canvas 等 API (色块方案已验证的路线)
- **进程隐藏**: libdevui_hide.so 在 JNI_OnLoad 里把自身 comm/cmdline 改成 netd
  (与 agent hide 同理念 — 只操作自身进程)
- **生命周期可控**: agent 记 pid 文件; stop = 优雅退出 (CmdFile quit → ShutdownHook
  清层) → 超时 SIGTERM; 绝不 pkill 通配 (防误杀系统进程, 见 git log)
- **进程序言**: 由 agent setsid 拉起 (脱离会话, 免受服务进程组清理波及)

## 构建 (build 机)

```powershell
# 1. 字库 (一次即可; 有产物可跳过)
cd D:\dsh\devctl\ui\tools & javac GenFont.java & java -Djava.awt.headless=true GenFont
# 2. 全量构建
powershell -ExecutionPolicy Bypass -File D:\dsh\devctl\ui\build.ps1
# 产物: dist/{devui.dex, libdevui_hide.so, devfont.bin}
```

## 部署与使用 (控制端)

```sh
python3 client/devctl.py devui dev push     # 推三个产物到 /data/local/tmp/devctl/
python3 client/devctl.py devui dev start    # 启动悬浮控件 (agent setsid 拉起)
python3 client/devctl.py devui dev status   # 运行状态
python3 client/devctl.py devui dev open     # 展开二级面板 (CmdFile)
python3 client/devctl.py devui dev close    # 收起回到球
python3 client/devctl.py devui dev refresh  # 刷新面板信息
python3 client/devctl.py devui dev stop     # 优雅停止 (层自动清理)
```

## DevUI API 一览 (ui/lib/com/devctl/ui/DevUI.java)

| API | 说明 |
|---|---|
| `DevUI.init()` | 反射初始化 (幂等) |
| `DevUI.screenW()/screenH()` | 屏幕尺寸 (wm size) |
| `DevUI.layer(name, w, h, z)` | 创建悬浮层 (show + 初始屏外) |
| `Layer.move(x,y)/hide()/remove()` | 移动/隐藏/移除层 |
| `Layer.lock()/unlock(c)` | 绘制 (返回 Canvas) |
| `DevUI.color(c,color)` / `rect(c,color,x1,y1,x2,y2)` / `bitmap(...)` | 绘制原语 (无 Paint) |
| `DevUI.loadFont(path)` → GlyphFont | 点阵字库 (drawText/drawTextCentered/drawTextInRect) |
| `DevUI.touch()` → TouchInput | 触摸输入 (Listener 回调: onDown/onMove/onUp(tap)) |
| `DevUI.watchCmd(path, handler)` | 控制通道 (文件轮询 300ms) |

## 设备端坑位记录 (全部实测踩过)

1. **Typeface/Paint font 在 app_process 下 native 断言崩溃** (gDefaultTypeface null)
   → 一切文字走预渲染点阵字库 (drawBitmap), 禁止 drawText/measureText
2. **匿名内部类回调写 `onDown(x,y)` 会命中自身** → StackOverflowError →
   RuntimeInit 杀进程 (signal 9, 表现为"莫名被杀") → 必须写 `DevctlOverlay.onDown(...)`
3. **d8 只喂主类会漏内部类** → ClassNotFoundException → 用通配 `*.class`
4. **静态二进制 TLS 对齐 8<64 被 bionic 拒载** → 动态链接
5. **dumpenvironment 服务循环重启, 连坐杀进程组** → setsid 拉起
6. **pkill -f app_process 会误杀 zygote/system_server 全家** → 只按 pid 操作
7. **getevent 坐标与屏幕比例映射** (touch max 19456x42240 → ÷16)
8. **Canvas.drawBitmap(Bitmap, Rect, RectF, Paint)** 反射绑定 float 签名会
   IllegalArgumentException → 用正确签名 (Rect/RectF 版)
