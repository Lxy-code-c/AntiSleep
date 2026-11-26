# AntiSleep · 防息屏助手

一个极简的防息屏小工具：让屏幕不熄、系统不睡，同时在后台帮你盯住指定的应用——**即使屏幕已锁定，这些应用的后台任务也能继续跑完**。

- 🖥 macOS：菜单栏常驻（SwiftUI，通用二进制）
- 🪟 Windows：无边框深色悬浮窗 + 系统托盘（Go + 纯 Win32，单文件 exe，无第三方依赖）
- 🔒 零依赖、不改系统设置、不写多余注册表（仅"开机自启"一项）

---

## ✨ 这次的重点：后台守护应用

以前的版本只能"整机防息屏一段时间"。这次加了**白名单守护**：

把下载器、视频渲染、代码编译、备份、模型训练这类"跑得久"的应用加进守护清单。只要清单里的任何一个应用还在运行，AntiSleep 就会持续向系统声明"别睡"，于是：

- 你合上盖子、锁屏去吃饭，任务照跑；
- 屏幕可以正常关闭/锁定（省电），但系统不会进入睡眠、不会挂起进程；
- 应用退出后，守护自动解除，不残留。

| 状态 | 行为 |
|---|---|
| 手动防息屏 | 屏幕不关 + 系统不睡 + 阻止屏保（最强） |
| 后台守护（白名单） | 屏幕可关、可锁，但**系统不睡**（任务继续） |

两种状态可以叠加：先"开始防息屏"，同时也盯着守护清单。

---

## ⬇️ 下载

到 [Releases](../../releases) 页面下载：

- `AntiSleep-macOS.zip` —— 解压得到 `AntiSleep.app`（macOS 13+，Apple 芯片 / Intel 通用）
- `AntiSleep.exe` —— Windows 10/11 64 位，双击即用，无需安装

### macOS 首次打开

应用是 ad-hoc 签名的本地构建，第一次打开会被 Gatekeeper 拦一下：

1. 解压后把 `AntiSleep.app` 拖进"应用程序"文件夹；
2. **右键 → 打开**（或到"系统设置 → 隐私与安全性"点"仍要打开"）；
3. 打开后图标出现在菜单栏右上角，点一下就是控制面板。

> 只想看源码/自己编译：`cd macOS && ./build.sh`

### Windows 首次运行

未做代码签名，SmartScreen 可能提示"未知发布者"：点"更多信息 → 仍要运行"即可。

- 点右上角 `×` 是收进托盘（防息屏继续生效）；
- 托盘图标右键可"显示窗口 / 开始 / 停止 / 开机自启 / 退出"；
- 关机或注销才能真正停止（这是系统行为，任何工具都拦不住）。

---

## 🕹 用法

### macOS

1. 菜单栏点盾牌图标；
2. 输入分钟数（1–1440），或点 `30 / 60 / 120 / ∞` 快捷值；
3. 点"开始防息屏"；`∞` 是一直开到手动停止；
4. "后台守护应用"区点 `＋ 添加`，从正在运行的 App 里选，或"选择 .app…"从磁盘挑；
5. 绿点 = 该应用正在运行、守护已生效。

### Windows

1. 双击 `AntiSleep.exe`；
2. 时长框填分钟，或点快捷芯片；点"开始防息屏"；
3. "后台守护应用"点 `＋ 添加`，从菜单里挑正在运行的程序，或"手动输入进程名…"（如 `chrome.exe`）；
4. 绿点 = 进程在跑，守护生效；
5. 勾"开机自启"会以 `--tray` 参数静默启动到托盘。

---

## ⚙️ 实现原理

**macOS**：调用系统自带 `/usr/bin/caffeinate`。

- 主防息屏：`caffeinate -d -i -m -s -u -t <秒>`（显示器 / 空闲 / 磁盘 / 系统 / 用户活跃全部挡住）
- 守护单个应用：`caffeinate -i -w <pid>` —— 只挡系统睡眠，显示器该关就关，进程一退出就自动失效
- 用 `NSWorkspace` 监听运行中的 App，白名单按 bundle id 匹配
- 自身用 `ProcessInfo.beginActivity` 防 App Nap，保证倒计时不被节流

**Windows**：`SetThreadExecutionState`。

- 主防息屏：`ES_CONTINUOUS | ES_DISPLAY_REQUIRED | ES_SYSTEM_REQUIRED`
- 后台守护：只用 `ES_CONTINUOUS | ES_SYSTEM_REQUIRED`，交给系统关屏/锁屏，但阻止睡眠（含 Modern Standby）
- 用 `CreateToolhelp32Snapshot` 枚举进程匹配白名单，每秒重申一次状态
- 配置存于 `%APPDATA%\AntiSleep\config.ini`；开机自启只写 `HKCU\...\Run\AntiSleep` 一个值

---

## 📁 仓库结构

```
AntiSleep/
├── macOS/
│   ├── App.swift        # 全部逻辑与界面
│   ├── Info.plist
│   └── build.sh         # 一条命令出通用二进制 .app + zip
├── windows/
│   ├── main.go          # 窗口、消息循环
│   ├── ui.go            # 自绘界面、托盘、自启、输入小窗
│   ├── state.go         # 配置、进程枚举、防息屏执行状态
│   ├── api.go           # Win32 API 声明
│   └── go.mod
└── LICENSE
```

Windows 构建：

```bat
cd windows
set GOOS=windows&& set GOARCH=amd64&& go build -ldflags="-H windowsgui -s -w" -o AntiSleep.exe .
```

---

## ⚠️ 边界

- 不阻止关机、注销、手动睡眠（"开始 → 睡眠"）；这类操作系统优先级最高。
- 不能保证某个第三方 App 一定恢复后台任务——那取决于该 App 自己是否声明了后台任务；AntiSleep 能保证的是**系统不睡、不休眠、不挂起**。
- macOS 上 `-s`（阻止系统睡眠）在电池供电时可能被系统忽略，接电源更稳。
- 两者都不需要管理员权限。

## License

MIT © Lxy-code-c
