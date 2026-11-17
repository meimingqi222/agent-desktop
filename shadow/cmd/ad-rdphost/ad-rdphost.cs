// ad-rdphost: hosts the mstscax ActiveX control connected to a WTS child
// session. The control must stay alive for the child session to remain
// connected; this process is the parent-side anchor of the shadow desktop.
//
// stdio protocol, one JSON object per line:
//   out: {"event":"ready","session_id":N}
//   out: {"event":"error","message":"..."}
//   in : {"cmd":"shutdown"}   -> Disconnect + exit
//   in : {"cmd":"show"|"hide"} -> toggle the viewer window
//
// The same JSON-line commands are also served on the named pipe
// \\.\pipe\ad-rdphost-<parentSessionId> so any later `ad` invocation
// can toggle the viewer after the spawning process has exited.
using System;
using System.Diagnostics;
using System.IO;
using System.IO.Pipes;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Windows.Forms;
using System.Threading;
using System.Drawing;

[ComImport, Guid("302d8188-0052-4807-806a-362b628f9ac5")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IExtSettings {
    [PreserveSig] int put_Property([MarshalAs(UnmanagedType.BStr)] string name, IntPtr pValue);
    [PreserveSig] int get_Property([MarshalAs(UnmanagedType.BStr)] string name, IntPtr pValue);
}

[ComImport, Guid("4f6996d5-d7b1-412c-b0ff-063718566907")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IIMsRdpClientNonScriptable5 {
    // IMsTscNonScriptable
    [PreserveSig] int put_ClearTextPassword([MarshalAs(UnmanagedType.BStr)] string _arg1);
    [PreserveSig] int put_PortablePassword([MarshalAs(UnmanagedType.BStr)] string pPortablePass);
    [PreserveSig] int get_PortablePassword(IntPtr pPortablePass);
    [PreserveSig] int put_PortableSalt([MarshalAs(UnmanagedType.BStr)] string pPortableSalt);
    [PreserveSig] int get_PortableSalt(IntPtr pPortableSalt);
    [PreserveSig] int put_BinaryPassword([MarshalAs(UnmanagedType.BStr)] string pBinaryPassword);
    [PreserveSig] int get_BinaryPassword(IntPtr pBinaryPassword);
    [PreserveSig] int put_BinarySalt([MarshalAs(UnmanagedType.BStr)] string pSalt);
    [PreserveSig] int get_BinarySalt(IntPtr pSalt);
    [PreserveSig] int raw_ResetPassword();
    // IMsRdpClientNonScriptable
    [PreserveSig] int raw_NotifyRedirectDeviceChange(IntPtr wParam, IntPtr lParam);
    [PreserveSig] int raw_SendKeys(int numKeys, IntPtr pbArrayKeyUp, IntPtr plKeyData);
    // IMsRdpClientNonScriptable2
    [PreserveSig] int put_UIParentWindowHandle(IntPtr phwndUIParentWindowHandle);
    [PreserveSig] int get_UIParentWindowHandle(IntPtr phwndUIParentWindowHandle);
    // IMsRdpClientNonScriptable3
    [PreserveSig] int put_ShowRedirectionWarningDialog(short pfShowRdrDlg);
    [PreserveSig] int get_ShowRedirectionWarningDialog(IntPtr pfShowRdrDlg);
    [PreserveSig] int put_PromptForCredentials(short pfPrompt);
    [PreserveSig] int get_PromptForCredentials(IntPtr pfPrompt);
    [PreserveSig] int put_NegotiateSecurityLayer(short pfNegotiate);
    [PreserveSig] int get_NegotiateSecurityLayer(IntPtr pfNegotiate);
    [PreserveSig] int put_EnableCredSspSupport(short pfEnableSupport);
    [PreserveSig] int get_EnableCredSspSupport(IntPtr pfEnableSupport);
    [PreserveSig] int put_RedirectDynamicDrives(short pfRedirectDynamicDrives);
    [PreserveSig] int get_RedirectDynamicDrives(IntPtr pfRedirectDynamicDrives);
    [PreserveSig] int put_RedirectDynamicDevices(short pfRedirectDynamicDevices);
    [PreserveSig] int get_RedirectDynamicDevices(IntPtr pfRedirectDynamicDevices);
    [PreserveSig] int get_DeviceCollection(IntPtr ppDeviceCollection);
    [PreserveSig] int get_DriveCollection(IntPtr ppDeviceCollection);
    [PreserveSig] int put_WarnAboutSendingCredentials(short pfWarn);
    [PreserveSig] int get_WarnAboutSendingCredentials(IntPtr pfWarn);
    [PreserveSig] int put_WarnAboutClipboardRedirection(short pfWarn);
    [PreserveSig] int get_WarnAboutClipboardRedirection(IntPtr pfWarn);
    [PreserveSig] int put_ConnectionBarText([MarshalAs(UnmanagedType.BStr)] string pConnectionBarText);
    [PreserveSig] int get_ConnectionBarText(IntPtr pConnectionBarText);
    // IMsRdpClientNonScriptable4
    [PreserveSig] int put_RedirectionWarningType(IntPtr pWrnType);
    [PreserveSig] int get_RedirectionWarningType(IntPtr pWrnType);
    [PreserveSig] int put_MarkRdpSettingsSecure(short pfRdpSecure);
    [PreserveSig] int get_MarkRdpSettingsSecure(IntPtr pfRdpSecure);
    [PreserveSig] int put_PublisherCertificateChain(IntPtr pVarCert);
    [PreserveSig] int get_PublisherCertificateChain(IntPtr pVarCert);
    [PreserveSig] int put_WarnAboutPrinterRedirection(short pfWarn);
    [PreserveSig] int get_WarnAboutPrinterRedirection(IntPtr pfWarn);
    [PreserveSig] int put_AllowCredentialSaving(short pfAllowSave);
    [PreserveSig] int get_AllowCredentialSaving(IntPtr pfAllowSave);
    [PreserveSig] int put_PromptForCredsOnClient(short pfPromptForCredsOnClient);
    [PreserveSig] int get_PromptForCredsOnClient(IntPtr pfPromptForCredsOnClient);
    [PreserveSig] int put_LaunchedViaClientShellInterface(short pfLaunchedViaClientShellInterface);
    [PreserveSig] int get_LaunchedViaClientShellInterface(IntPtr pfLaunchedViaClientShellInterface);
    [PreserveSig] int put_TrustedZoneSite(short pfIsTrustedZone);
    [PreserveSig] int get_TrustedZoneSite(IntPtr pfIsTrustedZone);
    // IMsRdpClientNonScriptable5
    [PreserveSig] int put_UseMultimon(short pfUseMultimon);
    [PreserveSig] int get_UseMultimon(IntPtr pfUseMultimon);
    [PreserveSig] int get_RemoteMonitorCount(IntPtr pcRemoteMonitors);
    [PreserveSig] int raw_GetRemoteMonitorsBoundingBox(IntPtr pLeft, IntPtr pTop, IntPtr pRight, IntPtr pBottom);
    [PreserveSig] int get_RemoteMonitorLayoutMatchesLocal(IntPtr pfRemoteMatchesLocal);
    [PreserveSig] int put_DisableConnectionBar(short _arg1);
    [PreserveSig] int put_DisableRemoteAppCapsCheck(short pfDisableRemoteAppCapsCheck);
    [PreserveSig] int get_DisableRemoteAppCapsCheck(IntPtr pfDisableRemoteAppCapsCheck);
    [PreserveSig] int put_WarnAboutDirectXRedirection(short pfWarn);
    [PreserveSig] int get_WarnAboutDirectXRedirection(IntPtr pfWarn);
    [PreserveSig] int put_AllowPromptingForCredentials(short pfAllow);
    [PreserveSig] int get_AllowPromptingForCredentials(IntPtr pfAllow);
}

[ComImport, Guid("b3378d90-0728-45c7-8ed7-b6159fb92219")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IIMsRdpClientNonScriptable3 {
    // IMsTscNonScriptable
    [PreserveSig] int put_ClearTextPassword([MarshalAs(UnmanagedType.BStr)] string _arg1);
    [PreserveSig] int put_PortablePassword([MarshalAs(UnmanagedType.BStr)] string pPortablePass);
    [PreserveSig] int get_PortablePassword(IntPtr pPortablePass);
    [PreserveSig] int put_PortableSalt([MarshalAs(UnmanagedType.BStr)] string pPortableSalt);
    [PreserveSig] int get_PortableSalt(IntPtr pPortableSalt);
    [PreserveSig] int put_BinaryPassword([MarshalAs(UnmanagedType.BStr)] string pBinaryPassword);
    [PreserveSig] int get_BinaryPassword(IntPtr pBinaryPassword);
    [PreserveSig] int put_BinarySalt([MarshalAs(UnmanagedType.BStr)] string pSalt);
    [PreserveSig] int get_BinarySalt(IntPtr pSalt);
    [PreserveSig] int raw_ResetPassword();
    // IMsRdpClientNonScriptable
    [PreserveSig] int raw_NotifyRedirectDeviceChange(IntPtr wParam, IntPtr lParam);
    [PreserveSig] int raw_SendKeys(int numKeys, IntPtr pbArrayKeyUp, IntPtr plKeyData);
    // IMsRdpClientNonScriptable2
    [PreserveSig] int put_UIParentWindowHandle(IntPtr phwndUIParentWindowHandle);
    [PreserveSig] int get_UIParentWindowHandle(IntPtr phwndUIParentWindowHandle);
    // IMsRdpClientNonScriptable3
    [PreserveSig] int put_ShowRedirectionWarningDialog(short pfShowRdrDlg);
    [PreserveSig] int get_ShowRedirectionWarningDialog(IntPtr pfShowRdrDlg);
    [PreserveSig] int put_PromptForCredentials(short pfPrompt);
    [PreserveSig] int get_PromptForCredentials(IntPtr pfPrompt);
    [PreserveSig] int put_NegotiateSecurityLayer(short pfNegotiate);
    [PreserveSig] int get_NegotiateSecurityLayer(IntPtr pfNegotiate);
    [PreserveSig] int put_EnableCredSspSupport(short pfEnableSupport);
    [PreserveSig] int get_EnableCredSspSupport(IntPtr pfEnableSupport);
    [PreserveSig] int put_RedirectDynamicDrives(short pfRedirectDynamicDrives);
    [PreserveSig] int get_RedirectDynamicDrives(IntPtr pfRedirectDynamicDrives);
    [PreserveSig] int put_RedirectDynamicDevices(short pfRedirectDynamicDevices);
    [PreserveSig] int get_RedirectDynamicDevices(IntPtr pfRedirectDynamicDevices);
    [PreserveSig] int get_DeviceCollection(IntPtr ppDeviceCollection);
    [PreserveSig] int get_DriveCollection(IntPtr ppDeviceCollection);
    [PreserveSig] int put_WarnAboutSendingCredentials(short pfWarn);
    [PreserveSig] int get_WarnAboutSendingCredentials(IntPtr pfWarn);
    [PreserveSig] int put_WarnAboutClipboardRedirection(short pfWarn);
    [PreserveSig] int get_WarnAboutClipboardRedirection(IntPtr pfWarn);
    [PreserveSig] int put_ConnectionBarText([MarshalAs(UnmanagedType.BStr)] string pConnectionBarText);
    [PreserveSig] int get_ConnectionBarText(IntPtr pConnectionBarText);
}

[UnmanagedFunctionPointer(CallingConvention.StdCall)]
public delegate int SetBoolPropDelegate(IntPtr self, [MarshalAs(UnmanagedType.LPStr)] string name, int value);
[UnmanagedFunctionPointer(CallingConvention.StdCall)]
public delegate int GetBoolPropDelegate(IntPtr self, [MarshalAs(UnmanagedType.LPStr)] string name, out int value);

public static class Kernel32 {
    [DllImport("kernel32.dll")] public static extern IntPtr VirtualQuery(IntPtr addr, out MEM_BASIC_INFORMATION mbi, UIntPtr len);
    [DllImport("kernel32.dll")] public static extern uint GetLastError();
    [StructLayout(LayoutKind.Sequential)]
    public struct MEM_BASIC_INFORMATION { public IntPtr BaseAddress, AllocationBase; public uint AllocationProtect; public IntPtr RegionSize; public uint State, Protect, Type; }
    public static bool Readable(IntPtr p, int len) {
        if (p == IntPtr.Zero) return false;
        MEM_BASIC_INFORMATION m;
        if (VirtualQuery(p, out m, (UIntPtr)Marshal.SizeOf(typeof(MEM_BASIC_INFORMATION))) == IntPtr.Zero) return false;
        return m.State == 0x1000 && (m.Protect & 0xEE) != 0 && m.RegionSize.ToInt64() >= len;
    }
    public static string ReadAnsi(IntPtr p) {
        if (!Readable(p, 4)) return null;
        try { return Marshal.PtrToStringAnsi(p); } catch { return null; }
    }
}

public class RdpAx : AxHost {
    public RdpAx() : base("{A3BC03A0-041D-42E3-AD22-882B7865C9C5}") {}
}

[ComImport, Guid("00020400-0000-0000-C000-000000000046")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IDisp {
    [PreserveSig] int GetTypeInfoCount(out int c);
    [PreserveSig] int GetTypeInfo(uint i, uint l, out IntPtr t);
    [PreserveSig] int GetIDsOfNames([In] ref Guid g, [MarshalAs(UnmanagedType.LPArray, ArraySubType=UnmanagedType.LPWStr)] string[] n, uint c, uint l, [Out, MarshalAs(UnmanagedType.LPArray)] int[] d);
    [PreserveSig] int Invoke(int id, [In] ref Guid g, uint l, short f, IntPtr p, IntPtr r, IntPtr e, IntPtr a);
}

static class P {
    [DllImport("wtsapi32.dll")] static extern bool WTSEnableChildSessions(bool e);
    [DllImport("wtsapi32.dll")] static extern bool WTSGetChildSessionId(out uint id);
    [DllImport("wtsapi32.dll")] static extern bool WTSIsChildSessionsEnabled();
    [DllImport("ole32.dll")] static extern int OleInitialize(IntPtr r);
    [DllImport("oleaut32.dll")] static extern int VariantClear(IntPtr v);
    [DllImport("kernel32.dll")] static extern uint SetErrorMode(uint mode);

    static IntPtr BoolVariant(bool v) {
        IntPtr p = Marshal.AllocCoTaskMem(16);
        for (int i = 0; i < 16; i++) Marshal.WriteByte(p, i, 0);
        Marshal.WriteInt16(p, 0, 11);
        Marshal.WriteInt16(p, 8, (short)(v ? -1 : 0));
        return p;
    }

    [STAThread]
    static int Main() {
        // Suppress csrss hard-error popups ("Exception Processing
        // Message"): a headless helper must never block on a modal
        // error dialog on the user's desktop.
        SetErrorMode(0x0001 | 0x0002 | 0x8000);
        OleInitialize(IntPtr.Zero);
        if (!WTSEnableChildSessions(true)) { EmitErr("WTSEnableChildSessions failed"); return 1; }

        int dw = 1600, dh = 900;
        {
            string w = Environment.GetEnvironmentVariable("AD_DISPLAY_W");
            string h = Environment.GetEnvironmentVariable("AD_DISPLAY_H");
            if (w != null) int.TryParse(w, out dw);
            if (h != null) int.TryParse(h, out dh);
            if (dw <= 0) dw = 1600;
            if (dh <= 0) dh = 900;
        }
        var form = new Form { Width = dw, Height = dh, Text = "ad shadow desktop", StartPosition = FormStartPosition.CenterScreen };
        theForm = form;
        var ax = new RdpAx { Dock = DockStyle.Fill };
        ((System.ComponentModel.ISupportInitialize)ax).BeginInit();
        form.Controls.Add(ax);
        ((System.ComponentModel.ISupportInitialize)ax).EndInit();
        // The mstscax control inside the form renders the child
        // session's desktop live: showing the form IS the viewer. It
        // stays hidden until `ad surface viewer` (or --viewer at open)
        // asks for it via the control pipe.
        if (Environment.GetEnvironmentVariable("AD_VIEWER") == "1") form.Show();

        object ocx = ax.GetOcx();

        var ext = (IExtSettings)ocx;
        {
            IntPtr pv = BoolVariant(true);
            int phr = ext.put_Property("ConnectToChildSession", pv);
            Marshal.FreeCoTaskMem(pv);
            if (phr != 0) { EmitErr("put_Property hr=0x" + phr.ToString("X8")); return 1; }
        }
        var t = ocx.GetType();
        var f = BindingFlags.Public | BindingFlags.Instance | BindingFlags.IgnoreCase;
        t.InvokeMember("DesktopWidth", f | BindingFlags.SetProperty, null, ocx, new object[]{dw});
        t.InvokeMember("DesktopHeight", f | BindingFlags.SetProperty, null, ocx, new object[]{dh});

        IntPtr pUnk = Marshal.GetIUnknownForObject(ocx);
        Guid iidTsc = new Guid("8C11EFAE-92C3-11D1-BC1E-00C04FA31489");
        IntPtr pObj;
        int qh = Marshal.QueryInterface(pUnk, ref iidTsc, out pObj);
        if (qh != 0) { EmitErr("QI IMsTscAx hr=0x" + qh.ToString("X8")); return 1; }
        IntPtr coreSet = IntPtr.Zero;
        for (int i = 0; i < 500; i++) {
            IntPtr slot = Marshal.ReadIntPtr(pObj, i * 8);
            if (!Kernel32.Readable(slot, 80)) continue;
            if ((uint)Marshal.ReadInt32(slot, 32) != 0xDBCAABCD) continue;
            if (Kernel32.ReadAnsi(Marshal.ReadIntPtr(slot, 24)) != "CTSPropertySet") continue;
            int cnt = Marshal.ReadInt32(slot, 72);
            IntPtr map = Marshal.ReadIntPtr(slot, 64);
            if (cnt < 100 || cnt > 500 || !Kernel32.Readable(map, cnt * 72)) continue;
            for (int j = 0; j < Math.Min(cnt, 10); j++) {
                if (Kernel32.ReadAnsi(Marshal.ReadIntPtr(map, j * 72)) == "ServerName") { coreSet = slot; break; }
            }
            if (coreSet != IntPtr.Zero) break;
        }
        if (coreSet == IntPtr.Zero) { EmitErr("core CTSPropertySet not found"); return 1; }
        {
            IntPtr vt = Marshal.ReadIntPtr(coreSet, 0);
            IntPtr fn = Marshal.ReadIntPtr(vt, 4 * 8);
            var del = Marshal.GetDelegateForFunctionPointer<SetBoolPropDelegate>(fn);
            int sh = del(coreSet, "ConnectToChildSession", 1);
            if (sh != 0) { EmitErr("SetBoolProperty hr=0x" + sh.ToString("X8")); return 1; }
        }
        var ns5 = (IIMsRdpClientNonScriptable5)ocx;
        ns5.put_AllowPromptingForCredentials(0);
        try {
            object adv = t.InvokeMember("AdvancedSettings", f | BindingFlags.GetProperty, null, ocx, null);
            adv.GetType().InvokeMember("EnableCredSspSupport", f | BindingFlags.SetProperty, null, adv, new object[]{true});
            adv.GetType().InvokeMember("SmartSizing", f | BindingFlags.SetProperty, null, adv, new object[]{true});
        } catch (Exception ex) { EmitErr("advsettings: " + ex.Message); return 1; }
        t.InvokeMember("Server", f | BindingFlags.SetProperty, null, ocx, new object[]{Environment.MachineName});


        var d = (IDisp)ocx;
        object ocx2store = ocx;
        Guid eg = Guid.Empty;
        int[] did = new int[1];
        int ghr = d.GetIDsOfNames(ref eg, new string[]{"Connect"}, 1, 0x409, did);
        int[] did2 = new int[1];
        d.GetIDsOfNames(ref eg, new string[]{"Server"}, 1, 0x409, did2);
        IntPtr pdp = Marshal.AllocCoTaskMem(32);
        for (int i = 0; i < 32; i++) Marshal.WriteByte(pdp, i, 0);
        IntPtr ei = Marshal.AllocCoTaskMem(64);
        for (int i = 0; i < 64; i++) Marshal.WriteByte(ei, i, 0);
        int chr = d.Invoke(did[0], ref eg, 0, 1, pdp, IntPtr.Zero, ei, IntPtr.Zero);
        if (chr == unchecked((int)0x80020009)) {
            IntPtr srcPtr = Marshal.ReadIntPtr(ei, 8);
            IntPtr descPtr = Marshal.ReadIntPtr(ei, 16);
            int scode = Marshal.ReadInt32(ei, 56);
            string desc = descPtr != IntPtr.Zero ? Marshal.PtrToStringBSTR(descPtr) : "";
            string srcs = srcPtr != IntPtr.Zero ? Marshal.PtrToStringBSTR(srcPtr) : "";
        }
        dispObj = ocx2store;
        {
            int[] dd2 = new int[1];
            d.GetIDsOfNames(ref eg, new string[]{"Disconnect"}, 1, 0x409, dd2);
            dispidDisconnect = dd2[0];
        }
        new Thread(WaitChild) { IsBackground = true }.Start();
        new Thread(ReadCommands) { IsBackground = true }.Start();
        new Thread(WatchChild) { IsBackground = true }.Start();
        new Thread(PipeServer) { IsBackground = true }.Start();
        Application.Run(form);
        return 0;
    }

    static volatile bool childReady;
    static int dispidDisconnect = -1;
    static Form theForm;

    static void DoShutdown() {
        try {
            var dd = (IDisp)dispObj;
            IntPtr dp = Marshal.AllocCoTaskMem(32);
            for (int i = 0; i < 32; i++) Marshal.WriteByte(dp, i, 0);
            dd.Invoke(dispidDisconnect, ref egRef, 0, 1, dp, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero);
        } catch {}
        Application.Exit();
    }

    static void OnUI(Action a) {
        try {
            if (theForm != null && theForm.InvokeRequired) theForm.Invoke(a);
            else a();
        } catch {}
    }

    // Dispatch handles one {"cmd":...} line from stdin or the control
    // pipe and returns the JSON ack written back to pipe clients.
    // "shutdown" is honored only from the owning process's stdin: the
    // pipe is reachable by any local process, so it must not be able
    // to tear down the session anchor.
    static string Dispatch(string line, bool allowShutdown) {
        if (line == null) return "{\"ok\":false,\"error\":\"empty\"}";
        if (line.Contains("\"shutdown\"")) {
            if (!allowShutdown) return "{\"ok\":false,\"error\":\"shutdown is not accepted on the control pipe\"}";
            DoShutdown();
            return "{\"ok\":true}";
        }
        if (line.Contains("\"show\"")) {
            OnUI(() => { theForm.Show(); theForm.WindowState = FormWindowState.Normal; theForm.Activate(); });
            return "{\"ok\":true}";
        }
        if (line.Contains("\"hide\"")) {
            OnUI(() => theForm.Hide());
            return "{\"ok\":true}";
        }
        return "{\"ok\":false,\"error\":\"unknown command\"}";
    }

    // PipeServer accepts viewer-control commands from any ad process of
    // this parent session, one connection at a time.
    static void PipeServer() {
        string name = "ad-rdphost-" + Process.GetCurrentProcess().SessionId;
        for (;;) {
            try {
                using (var pipe = new NamedPipeServerStream(name, PipeDirection.InOut, 1)) {
                    pipe.WaitForConnection();
                    var r = new StreamReader(pipe);
                    var w = new StreamWriter(pipe);
                    w.AutoFlush = true;
                    w.WriteLine(Dispatch(r.ReadLine(), false));
                }
            } catch { Thread.Sleep(200); }
        }
    }

    static void EmitErr(string m) {
        Console.WriteLine("{\"event\":\"error\",\"message\":\"" + m.Replace("\\", "\\\\").Replace("\"", "\\\"") + "\"}");
        Console.Out.Flush();
    }

    static void WaitChild() {
        for (int i = 0; i < 600 && !childReady; i++) {
            uint cid;
            if (WTSGetChildSessionId(out cid) && cid != 0xFFFFFFFF) {
                childReady = true;
                Console.WriteLine("{\"event\":\"ready\",\"session_id\":" + cid + "}");
                Console.Out.Flush();
                return;
            }
            Thread.Sleep(100);
        }
        Console.WriteLine("{\"event\":\"error\",\"message\":\"no child id\"}");
        Console.Out.Flush();
    }

    static void ReadCommands() {
        string line;
        while ((line = Console.In.ReadLine()) != null) {
            if (line.Contains("\"shutdown\"")) {
                Dispatch(line, true);
                return;
            }
        }
        // stdin EOF (the spawning ad.exe exited) is not a shutdown: the
        // child session must outlive ad so later invocations re-attach.
    }

    // WatchChild exits the process once the child session is gone, e.g.
    // after `ad surface close` logged it off.
    static void WatchChild() {
        while (!childReady) Thread.Sleep(200);
        for (;;) {
            uint cid;
            if (!WTSGetChildSessionId(out cid) || cid == 0xFFFFFFFF) {
                Console.WriteLine("{\"event\":\"session_gone\"}");
                Console.Out.Flush();
                Application.Exit();
                return;
            }
            Thread.Sleep(2000);
        }
    }
    static object dispObj;
    static Guid egRef = Guid.Empty;
}
