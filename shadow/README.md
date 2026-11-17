# agent-desktop-shadow (`ad`)

A Go host that gives an AI agent an **isolated virtual desktop** on Windows, per
[`docs/design.md`](docs/design.md). The host binary is `ad`; the in-session worker
binary is `ad-remote`. Both reuse `agent-desktop.exe` as an external process —
no fork, upstream upgrades freely.

## Build

```sh
cd shadow
go build -o bin/ad.exe ./cmd/ad
go build -o bin/ad-remote.exe ./cmd/ad-remote
# the RDP host helper is C# (hosts mstscax); build it with csc:
& 'C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe' /nologo /target:exe /platform:x64 `/
    /r:System.Windows.Forms.dll /r:System.Drawing.dll `
    /out:bin\ad-rdphost.exe cmd\ad-rdphost\ad-rdphost.cs
```

Requires Go 1.26+ and `agent-desktop` on the machine (npm package
`agent-desktop`, or set `AD_AGENT_DESKTOP_BIN` / `--agent-desktop-bin` to the
binary path).

## CLI

```text
ad <agent-desktop command> [args] [--surface main|pip]
ad status
ad surface open [--display WxH] [--viewer] [--username U] [--domain D]
ad surface switch --target main|pip [--auth-token T]
ad surface close
ad surface status
ad surface viewer [--on|--off]
ad mcp serve [--stdio | --sse addr]
```

Every command emits the agent-desktop JSON envelope plus `data.surface`.
Arguments after the command name pass through to `agent-desktop.exe` verbatim,
so the full upstream command surface (`snapshot`, `click`, `set-value`,
`screenshot`, `launch`, `list-windows`, `wait`, `batch`, ...) works unchanged:

```sh
ad status
AD_AUTH_TOKEN=tok ad snapshot --app notepad.exe -i --surface main --auth-token tok
AD_AUTH_TOKEN=tok ad set-value @s2hektu6eaeu0s:e1 "hello" --surface main --auth-token tok
```

### Flags

| flag | env | meaning |
|---|---|---|
| `--surface` | — | target plane; default `pip` |
| `--agent-desktop-bin` | `AD_AGENT_DESKTOP_BIN` | explicit agent-desktop binary |
| `--auth-token` | `AD_AUTH_TOKEN` | grant for main-surface commands and `--target main` |
| — | `AD_SHADOW_USERNAME` / `AD_SHADOW_PASSWORD` | optional credential precheck for `surface open` |

`surface open` never takes a password on the command line; when
`AD_SHADOW_PASSWORD` is set it is verified first. Child sessions auto-log
on as the current user, so a password is not required to create one.
Commands on `--surface main` always require a valid auth token
(`AUTH_REQUIRED` otherwise).

## MCP

```sh
ad mcp serve            # stdio (Claude Desktop / Zed style)
ad mcp serve --sse 127.0.0.1:8788
```

Tools: `desktop_status`, `desktop_snapshot`, `desktop_click`, `desktop_type`,
`desktop_press`, `desktop_mouse`, `desktop_screenshot`, `desktop_launch`,
`desktop_clipboard_get`, `desktop_clipboard_set`, `desktop_wait`,
`desktop_surface_open`, `desktop_surface_switch`, `desktop_surface_close`,
`desktop_surface_viewer`, and `desktop_exec` — an escape hatch that forwards
any agent-desktop command (`list-windows`, `scroll`, `drag`, `find`, `batch`,
...) so upstream additions need no rebuild. Each takes a `surface` argument
(default `pip`). `desktop_surface_open` needs no credentials: child sessions
auto-log on as the calling user.

## Agent integration

Claude Desktop / Zed-style config:

```json
{
  "mcpServers": {
    "agent-desktop-shadow": {
      "command": "C:\\path\\to\\ad.exe",
      "args": ["mcp", "serve"]
    }
  }
}
```

Agents without MCP can call the CLI directly — every command prints the
agent-desktop JSON envelope (`version/ok/data/error` + `data.surface`).
`ad` is non-interactive: nothing prompts on a non-TTY stdin.

### Single-binary distribution

```powershell
scripts\build.ps1
```

builds `bin\ad.exe` with `ad-remote.exe` and `ad-rdphost.exe` embedded; on
first run it extracts them next to itself, so copying `ad.exe` alone is a
complete install (a normal `go build` still works — helpers are then
expected as siblings).

## Shadow session

`ad surface open` creates a real, isolated Windows **child session**
(Microsoft-documented `WTSEnableChildSessions` + the mstscax ActiveX
control's `ConnectToChildSession` property — the same mechanism Doubao's
`pip_main.exe` uses). Flow:

1. `WTSEnableChildSessions(true)` — persisted, idempotent, needs elevation.
2. `ad` registers `ad-remote.exe --child` on the user's Run key and spawns
   `ad-rdphost.exe`, which hosts the mstscax control; `Connect()` makes the
   child session materialize and auto-log on as the current user (no
   password needed).
3. `ad-rdphost` prints `{"event":"ready","session_id":N}`; `ad` waits
   for `ad-remote` to come up on `\\.\pipe\ad-pip-N`.
4. pip commands then run **inside** the child session through the worker,
   which execs `agent-desktop.exe` there.

The session is persistent: `ad` exits without tearing it down, so a later
`ad` re-attaches via the probe (`EnsureReady`). `ad surface close` removes
the Run key, logs the session off, and `ad-rdphost` exits itself once the
child session is gone. Verified end-to-end on Windows Server 2022:
`list-windows`, `snapshot` (UIA tree) and `screenshot` all executed inside
the shadow desktop.

### Dialog auto-closer

`ad-remote` runs a `dialog_auto_closer` loop inside the shadow session
(design.md v0.6): every 750 ms it scans top-level dialogs and dismisses
whitelisted system error UI — Windows Error Reporting (`WerFault*`),
hard-error boxes ("* - System Error", "Exception Processing Message"),
.NET unhandled-exception dialogs and the localized equivalents — by
pressing the safest dismiss button (`Don't send` / `Cancel` / `No` /
`Close` / `OK`, in that order; never affirmative or debug actions) or
`WM_CLOSE` as a fallback. A modal nobody can click would otherwise stall
every forwarded command. Application dialogs raised by the agent's own
apps are left untouched. Each dismissal is logged on the worker's
stderr; set `AD_DIALOG_CLOSER=0` to disable.

### Viewer

`ad-rdphost` already hosts the mstscax control, which renders the child
desktop natively — showing its window IS the live view into the shadow
session, no extra protocol needed. The window is hidden by default and
toggled per parent session:

- `ad surface viewer` / `ad surface viewer off` — show / hide the window
- `ad surface open --viewer` — show it as soon as the session is up

Toggle commands go over `\\.\pipe\ad-rdphost-<parentSessionId>`, so any
later `ad` invocation can drive the viewer even though the helper was
spawned by an earlier one. The window is interactive: clicks and keys
inside it reach the shadow desktop (a manual take-over path).

One quirk worth knowing: `put_Property("ConnectToChildSession")` alone is
not enough — mstscax also needs the property written through the core
`CTSPropertySet` (`SetBoolProperty`), found by scanning the COM object's
fields; `ad-rdphost.cs` does this. And never set
`PromptForCredentials`/explicit credentials — that kills the auto-logon
with disconnect reason 4408.

## Current milestone status

| piece | state |
|---|---|
| v0.1 main-surface commands | **done** — `ad` execs `agent-desktop.exe`, envelope + `data.surface` verified on notepad |
| v0.2 probe + credentials | **done** — TermService/edition probe, `LogonUserW` precheck, marker mutex, WTS session enumeration and shadow-session detection are real |
| v0.3 IPC | **done** — `ad-remote` worker + `u32`+JSON named-pipe protocol, verified across sessions (console → child) |
| shadow-session create | **done** — WTS child sessions via `ad-rdphost` + `ConnectToChildSession`; `surface open/close` verified end-to-end |
| viewer | **done** — `ad surface viewer` toggles the live shadow-desktop window rendered by mstscax inside `ad-rdphost` |
| dialog_auto_closer | **done** — `ad-remote` dismisses whitelisted system error dialogs in the shadow session (`AD_DIALOG_CLOSER=0` to disable) |
| InputSink | seam (`internal/rdp`) — not needed: input reaches the child session through the in-session worker |
| authz gate | done — `surface switch --target main` requires `--auth-token` matching `AD_AUTH_TOKEN` |

## Layout

```text
cmd/ad/            CLI entry
cmd/ad-remote/     in-session worker (named-pipe server wrapping agent-desktop)
cmd/ad-rdphost/    C# helper hosting mstscax with ConnectToChildSession
internal/
  desktop/         envelope, service, surface router, main/pip adapters
  surface/         lifecycle state machine (Idle→Ready→Destroy)
  session/         probe, marker mutex, LogonUser check, WTS enum, child-session APIs, rdphost spawn
  ipc/             named-pipe framing, server, client, PID check
  mcp/             MCP server + desktop_* tools
  authz/           main-desktop token gate
  rdp/  remote/    seams for RDP loopback and worker injection
tests/pipecheck/   manual pipe round-trip smoke tool
```
