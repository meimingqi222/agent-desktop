---
module: agent-desktop-shadow
date: 2026-11-25
problem_type: runtime_error
tags: [windows, shadow, child-session, rdp, ipc, regression]
---

# Windows shadow child-session regressions

These decisions pin behavior that looks unnecessarily indirect but is required by Windows session security or by the actual child-session lifecycle.

## TermService probing must request query-only rights

Opening the service control manager with the Go `svc/mgr` package's default broad access makes `ad surface open` fail as an unprivileged user before the real child-session path is even tested. Querying `TermService` only needs `SC_MANAGER_CONNECT` on the manager and `SERVICE_QUERY_STATUS` on the service. Do not widen those rights for a probe.

Evidence: `shadow/internal/session/probe_windows_test.go`, test `TestTermServiceQueryDoesNotRequireElevation`.

## An RDP parent session is not a shadow child session

A normal RDP parent session has a WinStation name such as `RDP-Tcp#0`. It can still create a child session, so treating every non-`Console` session as a shadow session blocks the supported remote-parent case. Child-session identity comes from `WTSGetChildSessionId` for ownership and from the long hexadecimal WinStation name shape only for deciding whether the current process is inside a child session.

Evidence: `shadow/internal/session/childname_test.go`, test `TestIsChildWinStationName`; live verification created child sessions from `rdp-tcp#0` session 2.

## Unprivileged cross-session process launch is not a valid recovery plan

`WTSQueryUserToken` plus `CreateProcessAsUser` is the documented launch path, but Windows does not grant the needed token privileges to the tested unprivileged user. It remains a best-effort path for privileged hosts, not the required worker bootstrap. The supported recovery design is the Run-key `ad-remote --child` supervisor started by the child session's first logon, which then starts the pipe worker in the same session. The worker intentionally does not require a host PID: later `ad` invocations must be able to reconnect after the originating process exits.

Evidence: `shadow/cmd/ad-remote/config_test.go`, test `TestWorkerArgs`; live verification showed the supervisor and worker as separate processes inside the child session and successful reconnects from later `ad` processes.

## Reattach must preserve the resolved agent-desktop binary

Automatic reattach originally reopened the surface with empty options and rewrote the Run key without `--agent-desktop-bin`. A worker started after a later logon would lose the explicit binary even though the currently running worker had it. Open and reattach fill a missing binary from the resolved `MainAdapter` path, and explicit options always win over the default.

Evidence: `shadow/internal/surface/manager_test.go`, tests `TestReattachOpenOptsPreserveAgentDesktopBin` and `TestOpenOptsWithDefaultsOnlyFillMissingAgentDesktopBin`; live verification compared the Run key before and after a second `ad` invocation.

## mstscax memory scanning is not needed for ConnectToChildSession

The child-session connect works by writing the documented `ConnectToChildSession` extended property through the public COM interface and invoking `Connect` through `IDispatch` late binding. Scanning mstscax object memory for `CTSPropertySet` depended on private layout and could break on any Windows update. Do not reintroduce it as a shortcut. Credential prompting or explicit credentials must remain disabled because they break child-session auto-logon.

The RDP redirection surface is narrowed before connect: clipboard, drives, ports, printers, smart cards, Plug and Play devices, and dynamic devices are disabled. Clipboard isolation was verified live by writing in the child session and observing that the parent clipboard did not change.

Evidence: `shadow/cmd/ad-rdphost/ad-rdphost.cs`; live verification on Windows 11 Pro build 26200.

## The worker and agent-desktop children must not allocate consoles

Console-subsystem processes launched in the child session can create Windows Terminal windows there. Those windows steal focus and pollute `list-windows` and snapshots. `ad-remote` is built with the Windows GUI subsystem, and `MainAdapter` starts every per-command `agent-desktop.exe` child with `CREATE_NO_WINDOW`.

Evidence: final live smoke output listed only the fixture/user application windows; no `WindowsTerminal.exe` entry appeared while commands ran.

## The worker pipe is same-user IPC, not machine-wide IPC

A named pipe created with the platform default ACL can be reachable beyond the owning user. The worker pipe uses a security descriptor for the current user SID and SYSTEM because it carries commands that control applications inside the shadow session. The disabled host-PID check is a reconnect requirement, not permission for other users.

Evidence: `shadow/internal/ipc/pipe_windows_test.go`, test `TestCurrentUserPipeSecurityDescriptor`; `go test ./internal/ipc` passes with the restricted listener.

## Child-session ownership must survive process death

Pipes and the marker mutex disappear exactly when ownership information is most needed: after ad-rdphost, the supervisor, or the worker crashes. They are not ownership evidence even while present, because an orphaned Run key can start ad-remote inside a foreign child session and a manually started host can hold the marker. Without a durable record, a disconnected child session can become an unowned orphan that `ad surface close` refuses to touch. Creation therefore writes `HKCU\Software\AdShadow` values for the child session id and its initial WinStation name, and close deletes them only after the owned child is logged off.

Two WTS details make the match non-obvious. A newly created child can be visible through `WTSGetChildSessionId` before it appears in `WTSEnumerateSessions`, so recording retries briefly and queries `WTSWinStationName` directly. After the RDP host is killed, the still-logged-on child can remain enumerated with an empty WinStation name. In that state the exact current child session id is the strongest remaining evidence: `WTSGetChildSessionId` already scopes it to this parent, and refusing to act would leave an unrecoverable orphan. Never use the global marker mutex alone as proof that a pre-existing child belongs to ad.

Evidence: `shadow/internal/session/ownership_windows_test.go`, tests `TestOwnershipRecordSatisfied` and `TestChildOwnershipRecordRoundTrip`; the live smoke asserts that the record exists while the session is ready and is gone after close. Live verification killed both `ad-remote` processes and `ad-rdphost`, after which `surface status` still found the owned child and `surface close` logged it off and removed the record.

## End-to-end regression command

```powershell
cd shadow
go test ./...
scripts\smoke-windows.ps1 -AgentDesktopBin 'C:\path\to\agent-desktop-win32-x64.exe'
```

The live smoke test opens a real child session, verifies cross-process status discovery and window enumeration, launches the fixture application, snapshots and screenshots it, closes the surface, and checks that the child session and Run key are gone. The final verified run ended with `[smoke] PASS` on Windows 11 Pro as an unprivileged user in an RDP parent session.
