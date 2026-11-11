// SPDX-License-Identifier: GPL-3.0-or-later
//
// iec60870-5-interop IEC 60870-5-103 adapter for XVincentX/Open103: the
// control system, Open103's IEC8705103Manager (C++, header only), compiled
// for Linux unchanged with shim/linux.h (stand-ins for Windows names) and
// called through shim/shim.cpp, which also gives it a CommunicationPort over
// the TCP connection. Open103 is a master only: the image is a client. It
// implements the container contract in docs/CONTAINER_CONTRACT.md.

using System.Reflection;
using System.Runtime.InteropServices;
using System.Text;
using Interop103;

internal static class Program
{
    private const string Adapter = "open103";

    private static int Main(string[] argv)
    {
        Out.Adapter = Adapter;
        try
        {
            if (argv.Length == 0)
            {
                Console.Error.Write(ClientRunner.Usage(Adapter));
                return Exit.Usage;
            }

            switch (argv[0])
            {
                case "server":
                    throw new UsageException("server: this image has no server (Open103 is a master only)");
                case "client" when argv.Length >= 2:
                    return ClientRunner.Run(Adapter, argv[1], new Args(argv.Skip(2)), () => new Open103Link(),
                        what => what == "broadcast" ? "IEC8705103Manager sends to the link address it was made with only" : null);
                case "print-capabilities":
                    PrintCapabilities();
                    return Exit.Ok;
                case "print-fixture":
                    return ClientRunner.PrintFixture(argv.Length > 1 ? argv[1] : "baseline");
                case "version":
                    Console.WriteLine($"{Adapter} {Versions.Adapter} (XVincentX/Open103 {Versions.Upstream}, IEC 60870-5-103)");
                    return Exit.Ok;
            }

            Console.Error.Write(ClientRunner.Usage(Adapter));
            return Exit.Usage;
        }
        catch (UsageException e)
        {
            Out.Log(e.Message);
            return Exit.Usage;
        }
    }

    private static void PrintCapabilities() => Out.Emit(new Dictionary<string, object?>
    {
        ["schemaVersion"] = "1.0", ["adapter"] = Adapter, ["adapterVersion"] = Versions.Adapter,
        ["protocol"] = "iec60870-5-103", ["standard"] = "iec60870-5-103", ["transport"] = "tcp-stream",
        ["linkModes"] = new[] { "unbalanced" },
        ["upstream"] = new Dictionary<string, object?>
        {
            ["name"] = "XVincentX/Open103", ["version"] = Versions.Upstream, ["revision"] = Versions.Revision,
            ["license"] = "Apache-2.0", ["language"] = "C++", ["independentEngine"] = true,
        },
        ["roles"] = new Dictionary<string, object?> { ["server"] = false, ["client"] = true },
        ["features"] = ClientRunner.Features(gi: true, clockSync: true, command: true, ident: true, measurands: true, broadcast: false),
        ["pointTypes"] = Array.Empty<string>(), ["commandTypes"] = Array.Empty<string>(),
        ["asduTypes"] = new Dictionary<string, object?>
        {
            ["monitor"] = new[] { 1, 2, 4, 5, 9, 23, 26, 27, 28, 29, 30, 31 }, ["control"] = new[] { 6, 7, 20, 24, 25 },
        },
        ["clientOperations"] = new[] { "connect", "interrogate", "clock-sync", "command", "monitor" },
    });
}

internal static class Versions
{
    public static string Adapter => Attr("AdapterVersion") ?? "dev";
    public static string Upstream => Attr("UpstreamVersion") ?? "unknown";
    public static string Revision => Attr("UpstreamRevision") ?? "unknown";

    private static string? Attr(string key) => typeof(Versions).Assembly.GetCustomAttributes<AssemblyMetadataAttribute>()
        .FirstOrDefault(a => a.Key == key)?.Value;
}

internal static partial class Native
{
    private const string Lib = "libopen103.so";

    [LibraryImport(Lib, EntryPoint = "o103_open", StringMarshalling = StringMarshalling.Utf8)]
    public static partial IntPtr Open(string host, int port, int connectMs, int address, int serviceMs, byte[] err, int errlen);

    [LibraryImport(Lib, EntryPoint = "o103_connected")]
    public static partial int Connected(IntPtr h);

    [LibraryImport(Lib, EntryPoint = "o103_station_init")]
    public static partial int StationInit(IntPtr h, int budgetMs);

    [LibraryImport(Lib, EntryPoint = "o103_poll")]
    public static partial int Poll(IntPtr h, int cls, int budgetMs);

    [LibraryImport(Lib, EntryPoint = "o103_general_interrogation")]
    public static partial int GeneralInterrogation(IntPtr h, int scn, int budgetMs);

    [LibraryImport(Lib, EntryPoint = "o103_time_sync")]
    public static partial int TimeSync(IntPtr h, long unixSeconds, int budgetMs);

    [LibraryImport(Lib, EntryPoint = "o103_general_command")]
    public static partial int GeneralCommand(IntPtr h, int fun, int inf, int dco, int rii, int budgetMs);

    [LibraryImport(Lib, EntryPoint = "o103_frames")]
    public static partial int Frames(IntPtr h, byte[] buf, int len);

    [LibraryImport(Lib, EntryPoint = "o103_close")]
    public static partial void Close(IntPtr h);
}

/// <summary>The control system: IEC8705103Manager's calls; each result is the frames its port read.</summary>
internal sealed class Open103Link : ILink103
{
    private IntPtr _h;
    private int _budget;

    public bool Connected => _h != IntPtr.Zero && Native.Connected(_h) == 1;

    public void Open(string host, int port, int connectTimeoutMs, int linkAddress, int serviceTimeoutMs)
    {
        var err = new byte[256];
        _h = Native.Open(host, port, connectTimeoutMs, linkAddress, serviceTimeoutMs, err, err.Length);
        if (_h == IntPtr.Zero)
        {
            throw new IOException(Encoding.UTF8.GetString(err).TrimEnd('\0'));
        }

        // Open103 repeats a request until an answer fits it, and its start-up
        // polls class 1 until user data comes: a call gets a few services' time.
        _budget = Math.Max(serviceTimeoutMs * 4, 2000);
    }

    private LinkResponse? Result(int rc)
    {
        var buf = new byte[65536];
        var n = Native.Frames(_h, buf, buf.Length);
        var frames = new List<byte[]>();
        for (var i = 0; i + 2 <= n;)
        {
            var len = (buf[i] << 8) | buf[i + 1];
            frames.Add(buf[(i + 2)..(i + 2 + len)]);
            i += 2 + len;
        }

        if (frames.Count == 0)
        {
            return null;
        }

        static byte[]? Asdu(byte[] f) => f.Length > 8 && f[0] == 0x68 ? f[6..^2] : null;
        var last = frames[^1];
        var control = last[0] == 0x10 ? last[1] : last[0] == 0x68 && last.Length > 4 ? last[4] : (byte)0;
        var earlier = frames.Take(frames.Count - 1).Select(Asdu).OfType<byte[]>().ToList();
        _ = rc;
        return new LinkResponse(Asdu(last), (control & 0x20) != 0, last[0] != 0xE5 && (control & 0x0f) == 1, earlier);
    }

    public LinkResponse? StartLink() => Result(Native.StationInit(_h, _budget));
    public LinkResponse? Class1() => Result(Native.Poll(_h, 1, _budget));
    public LinkResponse? Class2() => Result(Native.Poll(_h, 2, _budget));
    public LinkResponse? GeneralInterrogation(int ca, int scn) => Result(Native.GeneralInterrogation(_h, scn, _budget));

    // TimeSync takes a time_t: whole seconds, the milliseconds are lost.
    public LinkResponse? ClockSync(int ca, DateTime time) =>
        Result(Native.TimeSync(_h, new DateTimeOffset(time, TimeSpan.Zero).ToUnixTimeSeconds(), _budget));

    public LinkResponse? GeneralCommand(int ca, int fun, int inf, int dco, int rii)
    {
        var rc = Native.GeneralCommand(_h, fun, inf, dco, rii, _budget);
        var r = Result(rc);
        if (rc == 0 && r is null)
        {
            // CommandTrasmission checks the command against the function
            // type of the identification and sends nothing when it does not fit.
            throw new NotSentException("Open103 did not send the command: it does not fit the function type the station identified with");
        }

        return r;
    }

    public void Dispose()
    {
        if (_h != IntPtr.Zero)
        {
            Native.Close(_h);
            _h = IntPtr.Zero;
        }
    }
}
