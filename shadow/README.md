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
```

Requires Go 1.26+ and `agent-desktop` on the machine (npm package
`agent-desktop`, or set `AD_AGENT_DESKTOP_BIN` / `--agent-desktop-bin` to the
binary path).

## CLI

```text
ad <agent-desktop command> [args] [--surface main|pip]
ad status
ad surface open [--display WxH] [--viewer] [--username U] [--password P] [--domain D]
ad surface switch --target main|pip [--auth-token T]
ad surface close
ad surface status
ad mcp serve [--stdio | --sse addr]
```

Every command emits the agent-desktop JSON envelope plus `data.surface`.
Arguments after the command name pass through to `agent-desktop.exe` verbatim,
so the full upstream command surface (`snapshot`, `click`, `set-value`,
`screenshot`, `launch`, `list-windows`, `wait`, `batch`, ...) works unchanged:

```sh
ad status
ad snapshot --app notepad.exe -i --surface main
ad set-value @s2hektu6eaeu0s:e1 "hello" --surface main
ad screenshot --surface main
```

### Flags

| flag | env | meaning |
|---|---|---|
| `--surface` | — | target plane; default `pip` |
| `--agent-desktop-bin` | `AD_AGENT_DESKTOP_BIN` | explicit agent-desktop binary |
| `--auth-token` | `AD_AUTH_TOKEN` | grant for `--target main` |
| — | `AD_SHADOW_USERNAME` / `AD_SHADOW_PASSWORD` | credentials for `surface open` |

## MCP

```sh
ad mcp serve            # stdio (Claude Desktop / Zed style)
ad mcp serve --sse 127.0.0.1:8788
```

Tools: `desktop_status`, `desktop_snapshot`, `desktop_click`, `desktop_type`,
`desktop_press`, `desktop_mouse`, `desktop_screenshot`, `desktop_launch`,
`desktop_clipboard_get`, `desktop_clipboard_set`, `desktop_wait`,
`desktop_surface_open`, `desktop_surface_switch`, `desktop_surface_close`.
Each takes a `surface` argument (default `pip`).

## Current milestone status

| piece | state |
|---|---|
| v0.1 main-surface commands | **done** — `ad` execs `agent-desktop.exe`, envelope + `data.surface` verified on notepad |
| v0.2 probe + credentials | **done** — TermService/edition probe, `LogonUserW` precheck, marker mutex, WTS session enumeration and shadow-session detection are real; `surface open` runs the state machine up to the RPC seam |
| v0.3 IPC | **done in isolation** — `ad-remote` worker + `u32`+JSON named-pipe protocol verified end-to-end on this host; cross-session attach pending |
| shadow-session create | **seam** — the TermService child-session RPC is undocumented; `Create` returns `TERM_SERVICE_UNAVAILABLE` and attach of a live shadow session returns a precise not-implemented error |
| RDP loopback / InputSink | seam (`internal/rdp`) |
| authz gate | done — `surface switch --target main` requires `--auth-token` matching `AD_AUTH_TOKEN` |

Until the RPC seam lands, `surface open` fails at `creating` with
`TERM_SERVICE_UNAVAILABLE` and pip commands fail with `SURFACE_NOT_READY`;
everything degrades to `--surface main`.

## Layout

```text
cmd/ad/            CLI entry
cmd/ad-remote/     in-session worker (named-pipe server wrapping agent-desktop)
internal/
  desktop/         envelope, service, surface router, main/pip adapters
  surface/         lifecycle state machine (Idle→Ready→Destroy)
  session/         probe, marker mutex, LogonUser check, WTS enum, create seam
  ipc/             named-pipe framing, server, client, PID check
  mcp/             MCP server + desktop_* tools
  authz/           main-desktop token gate
  rdp/  remote/    seams for RDP loopback and worker injection
tests/pipecheck/   manual pipe round-trip smoke tool
```
