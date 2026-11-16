# agent-desktop-shadow 设计文档

> 在 Windows 上为 AI agent 提供一个**隔离的虚拟桌面**：agent 在其中操作任意 Win32/UWP 应用（包括 ArcGIS Pro 这类重型软件），不干扰用户的真实桌面。
> 实现路线参考豆包电脑版 `aha_pip` 组件（pip_main.exe / pip_remote.dll / pip_sdk.dll）的已验证架构，执行引擎复用 `vercel-labs/agent-desktop` 的 UIA ref 语义层，宿主程序用 **Go** 实现，对外统一提供 **MCP + CLI** 入口。

- 状态：草案 v0.1
- 平台：Windows 10 1809+ / Windows 11（x64），优先 Pro/Enterprise SKU
- 语言：Go 1.22+（宿主），可选 ~200 行 C 薄层（仅 ActiveX 渲染宿主）
- 依赖：`agent-desktop.exe`（上游 Rust 产物，原样使用，不 fork）

---

## 1. 背景与动机

### 1.1 豆包电脑版的验证结论（逆向证据）

对 `%LOCALAPPDATA%\Doubao\Application\app\` 下二进制做字符串/导出/导入分析后确认，豆包的"虚拟桌面"实现为：

| 组件 | 文件 | 作用 |
|---|---|---|
| `pip_main.exe` | ProductName "豆包操作电脑" | 宿主进程。通过 **TermService RPC** 调 `CreateShadowSession` 创建本机第二个交互式登录会话；内嵌 **MsRdpClient ActiveX** 显示其画面；用 **IMsRdpInputSink** 注入键鼠 |
| `pip_remote.dll` | 注入到影子会话内 | `remote_process_launcher`（启动应用）、`shell_double_click_interceptor`、`dialog_auto_closer_service`、`explorer_startup_marker` |
| `pip_sdk.dll` | 对外 IPC | named pipe IPC（`pip_ipc`），Doubao 主进程与 pip_main 通信 |
| `vm_sdk` / `sbox` | 可选加固 | Detours API hook，对影子会话内进程做文件/网络 ACL 重定向 |
| `aha_cua.dll` | 操作引擎 | WGC/GDI 截图、UIA 树、SendInput、VirtualCursor 自绘光标 |

关键字符串证据：`CreateShadowSession`、`CreateShadowSession failed because TermService RPC is unavailable`、`An invisible shadow session exists and requires a password`、`IMsRdpClientNonScriptable`、`IMsRdpInputSink`、`OnSendMouseClickEvent`、`credential_guard_state`、`account_type`、`windows_hello`、`A shadow session cannot be created from a shadow session!`。

### 1.2 为什么是"影子会话"而不是其他方案

| 方案 | 问题 | 影子会话的优势 |
|---|---|---|
| `CreateDesktopEx` 隐藏 desktop | `SendInput` 只能打 input desktop，非输入桌面无法注入输入；后台桌面无法 WGC 截图 | 影子会话有完整 win32k session（winlogon/explorer/DWM），画面和输入都完整 |
| Hyper-V 实用 VM（HCS） | 本机已装软件不可见；需镜像/装配，ArcGIS Pro 这类软件不可用 | 影子会话共享真实 NT 卷与用户 profile，本机应用直接可见可跑 |
| 云电脑 RTC | 环境不是本机 | 同上 |
| 直接在主桌面操作 | 抢占用户光标/焦点，agent 与人互相干扰 | 输入走 RDP InputSink 只进影子会话，主桌面零干扰 |
| 新 Windows Virtual Desktop | shell 接口不稳定，无法定向截图/注入 | — |

代价：需要**当前用户的 Windows 凭据**（因为要完成真实 logon 才能拿到用户 profile）。这与豆包产品行为一致（"激活虚拟桌面要输 Windows 账号密码"）。

### 1.3 为什么和 agent-desktop 分离

- agent-desktop 的 `--session` 是**快照命名空间**（`sessions/<id>/` 目录），与 Windows 登录会话无关；在其内加"会话"概念会语义冲突。
- `PlatformAdapter` trait（`ObservationOps + ActionOps + InputOps + SystemOps`）假设进程就在目标会话里。跨会话操作需要"代理转发"，属于不同的抽象层。
- 不 fork 上游：agent-desktop 迭代快，`ad` 把它当**外部进程**调用，上游任意升级。

### 1.4 为什么宿主用 Go

- `ad` 的工作是进程/句柄/RPC/named pipe/COM 胶水，Go 的 goroutine + `x/sys/windows` 覆盖 99% 需求；`go build` 秒级，单 exe 分发。
- 唯一痛点是 ActiveX 容器（渲染画面才需要）：用 ~200 行 C 薄宿主 `axhost.exe` 或 `windows-ole` 类库解决；纯 headless 用法可以完全不渲染。

---

## 2. 目标与非目标

### 目标

1. 提供 `ad` 单二进制：`ad mcp serve`（MCP server）+ `ad <command>`（CLI），共享同一 `DesktopService`。
2. `ad surface open` 创建一个后台影子会话，agent 可在其中操作**本机已装的任意 GUI 应用**。
3. 操作不干扰主桌面（无焦点抢夺、无光标移动、无窗口弹到前台）。
4. agent 拿到的是**结构化 ref**（UIA 树），而非纯像素坐标——保留 agent-desktop 的核心价值。
5. 可选提供 PiP 悬浮窗让**用户**实时观看影子桌面。
6. `surface switch main|pip` 双平面模型，操作主桌面需显式授权。

### 非目标（v1 不做）

- 云电脑/远端桌面接入（`AhaMuleLocalSDK` 方向，另一产品线）。
- sbox/vm_sdk 式 Detours 文件网络重定向（v2 可选加固）。
- macOS/Linux。
- 重写 agent-desktop 的 UIA/ref/错误码体系。

---

## 3. 总体架构

```
┌── 调用方 ──────────────────────────────────────────────────┐
│  AI agent (MCP client)      人/脚本                         │
│     │                          │                          │
│     ▼                          ▼                          │
│  ad mcp serve (stdio/sse)   ad <cmd> (JSON envelope 输出)   │
└──────────────┬─────────────────────────────────────────────┘
               │
               ▼
        DesktopService (Go)
     ┌──────────────────────────────┐
     │ 路由: --surface main | pip    │
     └──────┬──────────────┬───────┘
            │              │
   surface=main     surface=pip
            │              │
            ▼              ▼
   agent-desktop.exe   SurfaceManager (Go)
   (用户会话内, 直接)   ├─ session/   影子会话生命周期
            │          │   probe, credentials, create, guard
            │          ├─ rdp/      RDP loopback 管理
            │          │   (可选 axhost.exe 渲染 PiP)
            │          ├─ remote/   in-session worker 注入与拉起
            │          └─ ipc/      named pipe 协议
            │              │
            │              ▼
            │      影子会话 (Session N, 同一用户)
            │      ├─ ad-remote.exe (注入/启动的会话内代理)
            │      │    ├─ agent-desktop.exe  ← 同一个二进制
            │      │    │   (UIA/ref/capture 全量复用)
            │      │    ├─ dialog_auto_closer
            │      │    └─ shell hook (可选)
            │      └─ explorer.exe + 用户的 ArcGIS Pro/Office/...
            │
            ▼
     用户在主桌面看到的: PiP 悬浮窗(可选) 或 什么都不显示
```

### 两个关键决定

1. **输入不走 SendInput，走 `IMsRdpInputSink`**。宿主通过 RDP ActiveX 的非脚本接口直接往影子会话输入栈写事件——这是"后台可操作、前台无感"的根本原因。headless ref 动作（click/set-value）仍在会话内走 UIA，不经过 InputSink；只有物理动作（drag/hover/mouse-*）走 InputSink。
2. **UIA 由"会话内 worker"产出**。`ad-remote.exe` 在影子会话内运行，承载 `agent-desktop.exe`；宿主通过 named pipe 把 `snapshot --app X` 转发过去，拿到 refmap JSON 再回给调用方。ref 的 `snapshot_id` 命名空间由 `ad` 统一管理，跨 surface 不混用。

---

## 4. 模块设计（Go 包）

```
cmd/ad/main.go                  # CLI 入口，cobra 或轻量手写 subcommand 分发
internal/
  desktop/
    service.go                  # DesktopService: 统一业务接口
    router.go                   # surface 路由 main|pip → adapter
    envelope.go                 # JSON envelope (版本/ok/data/error/disposition)
    adapter_main.go             # 主桌面: 直接 exec agent-desktop.exe
    adapter_pip.go              # 影子桌面: exec ad-remote 经 ipc
  surface/
    manager.go                  # SurfaceManager: pip 会话状态机
    state.go                    # Probe→CredentialPrecheck→Create→WaitLogon→Inject→Ready→Destroy
  session/
    probe.go                    # TermService 探针: SKU/服务状态/ChildSession 开关
    credential.go               # LogonUser 预校验; account_type(Local/MSA); CredGuard 检测
    create.go                   # CreateShadowSession (termsrv RPC, 逆向 winsta/termsrv)
    destroy.go                  # LogoffSession/标记 auto_destroy
    marker.go                   # shadow_session_marker: 全局互斥 + 复用发现
  rdp/
    client.go                   # IMsRdpClient 生命周期 (经 axhost 或内嵌)
    inputsink.go                # IMsRdpInputSink: SendMouse*/SendKeyboardEvent/SendKeyStroke
    events.go                   # RDP Events: PasswordRequest/LogonError/Disconnected
    settings.go                 # AdvancedSettings: NLA/重定向/分辨率/相对鼠标
  remote/
    inject.go                   # CreateRemoteThread+VirtualAllocEx 注入 ad-remote
    launcher.go                 # remote_process_launcher: 会话内启进程(create_in_vm/run_as_admin)
    dialogcloser.go             # 周期枚举影子会话顶层窗口, 自动确认/关闭系统对话框
    shellhook.go                # (可选) 拦截 explorer 双击, 转发成 agent 命令
  ipc/
    server.go                   # named pipe server (会话内 worker ↔ 宿主)
    protocol.go                 # 帧格式 + JSON-RPC, 兼容 gac_runtime 的(module,command)思路
  mcp/
    server.go                   # mcp-go / go-sdk server, tools 注册
    tools.go                    # tool 定义 → DesktopService 映射
  authz/
    gate.go                     # 权限门控: surface switch main 需要授权 token
    store.go                    # DPAPI 凭据缓存 (可选, 默认不存)
  axhost/                       # (可选, 只有渲染才用)
    axhost.go                   # C 薄宿主进程管理: 启动/HWND/resize/销毁
```

### DesktopService 接口（Go）

```go
type Surface string // "main" | "pip"

type DesktopService interface {
    Status(ctx) (*Status, error)
    Snapshot(ctx, s Surface, opts SnapshotOpts) (*Tree, error)   // UIA refmap
    Click(ctx, s Surface, ref string, opts ClickOpts) (*Result, error)
    Type(ctx, s Surface, ref string, text string) (*Result, error)
    Press(ctx, s Surface, keys []string) (*Result, error)
    Mouse(ctx, s Surface, ev MouseEvent) (*Result, error)        // 物理输入
    Screenshot(ctx, s Surface) (*Frame, error)                   // PNG
    Launch(ctx, s Surface, app string, opts LaunchOpts) (*Result, error)
    ClipboardGet/Set(ctx, s Surface, ...)

    SurfaceOpen(ctx, opts OpenOpts) (*SurfaceInfo, error)        // 建影子会话
    SurfaceClose(ctx, id string) error
    SurfaceSwitch(ctx, target Surface, token AuthToken) error    // main 需授权
    Wait(ctx, s Surface, pred WaitPredicate) (*Result, error)
}
```

---

## 5. 影子会话生命周期（状态机）

```
        ┌──────────┐
        │  Idle    │
        └────┬─────┘
   surface open
        ┌────▼─────────────────────────────────────────────┐
        │ 1. Probe                                          │
        │    - Termservice 服务可 RPC?                       │
        │    - SKU 支持 child session?                       │
        │    - 已有 shadow session marker? → attach 复用      │
        └────┬─────────────────────────────────────────────┘
        ┌────▼─────────────────────────────────────────────┐
        │ 2. CredentialPrecheck                             │
        │    - account_type: Local | MicrosoftAccount        │
        │    - LogonUser 校验密码 (不进会话, 只验证)          │
        │    - CredentialGuard 开启? → 必须交互式密码          │
        │    - has_password=false (纯 Hello) → 明确报错引导         │
        └────┬─────────────────────────────────────────────┘
        ┌────▼─────────────────────────────────────────────┐
        │ 3. CreateShadowSession                            │
        │    - TermService RPC (winsta/termsrv 未文档化接口)  │
        │    - 得到 session_id, 标记 marker mutex            │
        └────┬─────────────────────────────────────────────┘
        ┌────▼─────────────────────────────────────────────┐
        │ 4. Connect (RDP loopback)                         │
        │    - MsRdpClient put_Server → 本机 listener        │
        │    - 等待 [RDP Events] LoginComplete               │
        │    - explorer_startup_marker 轮询                  │
        └────┬─────────────────────────────────────────────┘
        ┌────▼─────────────────────────────────────────────┐
        │ 5. Inject                                         │
        │    - CreateRemoteThread → 加载 ad-remote.exe/dll   │
        │    - named pipe IPC server 握手                    │
        │    - dialog_auto_closer 启动                       │
        └────┬─────────────────────────────────────────────┘
        ┌────▼─────┐        ┌──────────┐
        │  Ready   │ ──────►│ Destroy  │  auto_destroy/显式 close
        └──────────┘        └──────────┘
```

### 复用与恢复

- `marker mutex`（全局命名对象）+ 注册表 marker 让第二次 `surface open` 直接 attach 已有影子会话（豆包策略：`An invisible shadow session exists and requires a password` → 用已缓存凭据或重新提示）。
- 宿主崩溃后下次启动自动发现孤儿影子会话并可选择接管或 `DestroyShadowSession`。

---

## 6. 三条通道

### 6.1 显示

| 模式 | 实现 | 用途 |
|---|---|---|
| headless（默认） | 不渲染；`screenshot` 由会话内 worker GDI/WGC 抓屏 → PNG → pipe | agent 只需要"看" |
| viewer | `axhost.exe` 内嵌 MsTscAx，作为独立 PiP 悬浮窗；或嵌入调用方 HWND | 用户观看 |
| thumbnail | 会话内 worker 周期抓屏推送给 MCP 客户端（resource 订阅） | 轻量预览 |

### 6.2 输入

- **语义路径**（默认）：ref 动作在会话内由 agent-desktop 走 UIA——不经过 RDP 通道，可靠性同主桌面。
- **物理路径**（`--headed` 等价语义）：`IMsRdpInputSink`——`SendMouseMoveEvent`/`SendMouseClickEvent`/`SendMouseWheelEvent`/`SendKeyboardEvent`/`SendKeyStroke`，作用于影子会话输入栈。
- `IMsRdpClientNonScriptable.put_KeyboardHookMode` 控制组合键被谁吃掉。

### 6.3 IPC

- 宿主 ↔ 会话内 worker：**named pipe**（`\\.\pipe\ad-pip-<sessionid>-<nonce>`），帧 = `u32 len + JSON`，请求/响应带 id；会话内 server 只接受宿主进程 PID（`GetNamedPipeClientProcessId` 校验 + 进程启动时间戳防复用攻击，参考 gac_runtime 的 `verify_server_identity`）。
- agent-desktop 协议兼容：`ad` 内部仍然对 worker 调 `agent-desktop.exe` 的 JSON envelope（`version/ok/command/data/error`），对外原样透传再叠加 surface 元数据。

---

## 7. 对外接口

### 7.1 CLI

```
ad status                              # 平台/权限/影子会话可用性/当前 surface
ad snapshot --app notepad.exe -i       # UIA 树+ref (当前 surface)
ad click @s8f3k2p9:e8
ad type @s8f3k2p9:e8 --text "hello"
ad screenshot [--surface pip]
ad launch --app "C:\...\ArcGISPro.exe" --surface pip
ad surface open [--display 1920x1080] [--viewer]
ad surface switch --target main        # 需 --auth-token 或交互授权
ad surface switch --target pip
ad surface close
ad clipboard get|set
ad wait --element @ref --predicate actionable
ad mcp serve [--stdio|--sse :port]
```

所有命令输出与 agent-desktop 一致的 JSON envelope，外加 `data.surface: "main"|"pip"`。

### 7.2 MCP tools

`ad mcp serve` 暴露与 CLI 一一对应的 tool（名字 `desktop_*`）：

`desktop_status`, `desktop_snapshot`, `desktop_click`, `desktop_type`, `desktop_press`, `desktop_mouse`, `desktop_screenshot`, `desktop_launch`, `desktop_clipboard_get/set`, `desktop_surface_open`, `desktop_surface_switch`, `desktop_surface_close`, `desktop_wait`

- 每个 tool 带 `surface` 参数（默认 `pip`）。
- `desktop_snapshot`/`desktop_screenshot` 返回 agent-desktop 风格 JSON；`desktop_screenshot` 同时返回 `resources`（PNG blob）供支持 media 的 client 显示。
- 支持 MCP `roots`/progress 通知的 client 可订阅影子桌面 frame 流（viewer 模式的 MCP 形态）。

### 7.3 与其他 agent 的接入

- **Zed / Claude Desktop / 通用 MCP client**：`"command": "ad", "args": ["mcp", "serve"]`
- **trae-acp**：`ad` 也可作为一个 ACP-visible 的 shell 工具被调用（直接 `ad <cmd>` 输出 JSON），或后续加 `ad acp serve` 适配层。
- **REST 场景**：`ad mcp serve --sse 127.0.0.1:PORT`（默认仅本机）。

---

## 8. 安全与权限模型

| 平面 | 默认 | 说明 |
|---|---|---|
| pip（影子会话） | 开放 | agent 的主战场；可配进程/文件白名单 |
| main（主桌面） | 需授权 | `surface switch main` 要求一次性授权 token（交互弹窗或 `--auth-token`），等价豆包 `MAIN_DESKTOP_PERMISSION` |
| 凭据 | 不持久化（默认） | 密码仅驻留内存用于会话创建；可选 DPAPI 加密缓存，永不落明文 |
| IPC | 本机 + PID 校验 | named pipe ACL 只允许宿主用户；校验对端 PID+启动时间 |
| 注入 | 代码签名 | ad-remote 必须 Authenticode 签名，降低 EDR 告警 |
| 锁屏 | 拒绝输入 | `Current Windows session is locked` 时物理输入直接拒绝（与豆包一致） |
| UAC 弹窗 | 不越权 | 影子会话里的 UAC/系统对话框由 `dialog_auto_closer` 按白名单处理，agent 不能静默提权 |

---

## 9. 兼容性矩阵与降级策略

| 环境 | 行为 |
|---|---|
| Win10/11 Pro+ 正常 | 全功能 |
| Credential Guard 开启 | 强制交互式密码输入（不提供缓存路径），与豆包一致 |
| 纯 Hello/PIN 账户（无密码） | 明确报错并引导设置密码（建会话必需） |
| Windows Home / TermService 不可用 | `surface open` 返回 `PLATFORM_NOT_SUPPORTED`，自动降级为 main-only（仍可跑 agent-desktop 全部功能，只是无隔离） |
| 会话创建 RPC 在系统更新后失效 | `session/probe` 每次运行先探测，失败回退 main-only 并给出 `TERM_SERVICE_UNAVAILABLE` |
| 无 axhost / 未启用 viewer | 纯 headless，所有功能仍可用（agent 不依赖渲染窗口） |

---

## 10. 风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| `CreateShadowSession`/TermService RPC 未文档化 | 高 | `term_service_probe` 探测 + 多版本适配表 + main-only 降级 |
| 会话注入与 API hook 被 EDR 拦截 | 中高 | Authenticode 签名、最小注入面、可选关闭 sandbox 层 |
| 凭据处理 | 中 | 内存驻留、不写盘、可选 DPAPI；绝不出现在命令行参数 |
| agent-desktop 上游 JSON envelope 变更 | 低 | 版本探测，封最低支持版本 |
| RDP ActiveX 在 Win11 24H2+ 行为变化 | 中 | 自研帧通道兜底（会话内 GDI/WGC 抓屏+PNG over pipe，已实现 `screenshot` 即可复用） |

---

## 11. 里程碑

| 版本 | 内容 | 验收 |
|---|---|---|
| v0.1 | `ad status`/`snapshot`/`click`/`type`/`screenshot`/`launch` on **main**，复用 agent-desktop | CLI 全套命令在 notepad 上跑通 observe→act→verify |
| v0.2 | `session/probe` + `CreateShadowSession` + 凭据预检 | `ad surface open` 创建出 session_id，`quser` 可见 |
| v0.3 | `ad-remote` 注入 + named pipe IPC + 会话内 agent-desktop 转发 | `ad snapshot --surface pip` 返回影子会话里的 UIA 树 |
| v0.4 | `IMsRdpInputSink` 输入 + `switch_desktop` + 权限门控 | agent 在影子桌面完成 ArcGIS Pro 打开→菜单操作→关闭，主桌面光标不动 |
| v0.5 | `ad mcp serve` 全量 tools | Zed/trae-acp 通过 MCP 完成一次完整任务 |
| v0.6 | axhost PiP 悬浮窗 + dialog_auto_closer + explorer marker | 用户可实时观看影子桌面 |
| v1.0 | 会话复用/崩溃恢复/EDR 测试/文档 | 生产可用 |

---

## 12. 目录建议

```
D:\code\agent-desktop-shadow\
├── go.mod
├── cmd\ad\main.go
├── internal\
│   ├── desktop\  surface\  session\  rdp\  remote\  ipc\  mcp\  authz\  axhost\
├── docs\design.md            ← 本文档
├── tools\axhost\             # 可选 C 薄宿主
└── tests\
```

## 附录 A：与豆包组件的对照表

| 豆包 | ad | 说明 |
|---|---|---|
| `pip_main.exe` | `ad`（Go 宿主） | 会话+显示+输入宿主 |
| `pip_remote.dll` | `ad-remote.exe` | 会话内代理 |
| `pip_sdk.dll` | `internal/ipc` | named pipe 协议 |
| `aha_cua.dll` | `agent-desktop.exe` | 操作引擎（更强：ref 语义） |
| `IMsRdpInputSink` | `internal/rdp/inputsink.go` | 同接口 |
| `switch_desktop(main|pip)` | `ad surface switch` | 同名概念 |
| `MAIN_DESKTOP_PERMISSION` | `authz` token 门控 | 同级控制 |
| `vm_sdk`/`sbox` | （v2 可选） | 暂不做 |
