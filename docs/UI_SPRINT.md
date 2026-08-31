# devctl 悬浮窗 — Sprint 计划 (任务分解)

> 基于 PRD (docs/UI_PRD.md), 按依赖顺序拆分任务。
> 每任务: 实现 → 本地/CI 构建 → 设备部署 → 截图断言 → 独立 commit。

## Sprint 1: 基础设施 (自动化测试闭环)

| # | 任务 | 产出 | 验收 |
|---|---|---|---|
| 1.1 | 自动化测试脚本 | tests/ui_test.sh | deploy/shot/status/full 可运行 |
| 1.2 | CI 构建产物下载脚本 | scripts/ci_fetch.sh | 从 GitHub Actions 拉 ui-dist |
| 1.3 | 截图断言脚本 | tests/assert_shot.py | 自动判读截图 (OCR/像素) |

## Sprint 2: 字库扩充 (GG 面板所需字符)

| # | 任务 | 产出 | 验收 |
|---|---|---|---|
| 2.1 | genfont.py (Pillow 版) | ui/tools/genfont.py | 生成 devfont.bin 格式合法 |
| 2.2 | 字符集扩充 | 261+ 字符 | 所需中文全在, 预览图可读 |
| 2.3 | 双生成器一致性 | GenFont.java 同步 | 字符表一致 |

## Sprint 3: UI 面板改造 (GG 风格)

| # | 任务 | 产出 | 验收 |
|---|---|---|---|
| 3.1 | 面板全屏化 | DevctlOverlay 改 PANEL=screen | 截图面板占满屏 |
| 3.2 | GG 深色主题 | 背景/标题/按钮配色 | 截图主题一致 |
| 3.3 | 本机信息区 | 型号/Android/agent 版本 | 截图可读 |
| 3.4 | 连接信息区 | 活跃连接数+列表 | 2客户端时数=2 |
| 3.5 | 自身通信状态 | agent 存活+时间戳 | 显示"正常" |

## Sprint 4: agent 侧数据供给

| # | 任务 | 产出 | 验收 |
|---|---|---|---|
| 4.1 | peers() 方法 | agent/peers.go | devctl run dev peers 返回 JSON |
| 4.2 | dash.json 状态文件 | agent 刷写 dash.json | 内容含连接数/时间戳 |

## Sprint 5: 集成与发布

| # | 任务 | 产出 | 验收 |
|---|---|---|---|
| 5.1 | 全流程集成 | full 测试全绿 | 所有断言通过 |
| 5.2 | 回归测试 | v0.5 功能不破坏 | 球/面板/刷新/退出 |
| 5.3 | 发布 | tag v0.6 + release | CI 构建全绿 |

## Sprint 3 补充: 横屏兼容 (2026-08-31 实机验证)

### 需求
- 游戏 (Minecraft) 强制横屏时悬浮窗须自适应
- 位置: 悬浮球跟随横屏右侧, 面板全屏横屏布局, 按钮不溢出

### 实现
| 组件 | 改动 |
|---|---|
| DevUI.java | `refreshScreenSize()` 公开; screenSize 优先读 `dumpsys window` mBounds (游戏 rotation), 兜底 wm size |
| DevctlOverlay.java | 主循环 2s 旋转检测 (W/H 变化→rebuildLayers); `layoutMetrics()` 横屏自适应 (标题栏/按钮按屏高比例); `truncateTo()` 文本行宽截断 |

### 关键坑
1. `wm size` 只报 Physical (1216x2640), **游戏横屏是 app 层 rotation** (`mBounds=Rect(0,0 - 2640,1216)`) → 起初检测不到旋转
2. 尝试 wm size override 模拟横屏 → **危险** (用户红线: 切勿改分辨率) → 改用地真实游戏 (MC) 横屏测试
3. mBounds grep 取第一处 (横屏窗口), 第二处是 0,0-0,0 隐藏窗口 (head -1 规避)

### 自动化验证 (MC 横屏实测)
- [x] 启动 MC → mRotation=ROTATION_90, screencap 2640x1216
- [x] 悬浮球横屏位置 (2450,405 右侧) 截图确认
- [x] 横屏面板全屏 + 标题栏/信息卡/按钮布局截图确认
