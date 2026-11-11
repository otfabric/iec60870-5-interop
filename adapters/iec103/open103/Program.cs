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
                    return ClientRunner.Run(Adapter, argv[1], new Args(argv.Skip(2)), () => new Open103Link(), what => what switch
                    {
                        "broadcast" => "IEC8705103Manager sends to the link address it was made with only",
                        "generic-read" or "generic-write" => "IEC8705103Manager has no generic services (GenericService() returns false)",
                        "disturbance-list" => "IEC8705103Manager has no request of the list (TOO 24): it reacts to the ASDU 23 the station sends",
                        _ when what.StartsWith("fun ", StringComparison.Ordinal) =>
                            "IEC8705103Manager sends its orders with the function type of the identification",
                        _ => null,
                    });
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
        ["features"] = ClientRunner.Features(gi: true, clockSync: true, command: true, ident: true, measurands: true, broadcast: false,
            disturbanceData: true),
        ["pointTypes"] = Array.Empty<string>(), ["commandTypes"] = Array.Empty<string>(),
        ["asduTypes"] = new Dictionary<string, object?>
        {
            ["monitor"] = new[] { 1, 2, 4, 5, 9, 23, 26, 27, 28, 29, 30, 31 }, ["control"] = new[] { 6, 7, 20, 24, 25 },
        },
        ["clientOperations"] = new[] { "connect", "interrogate", "clock-sync", "command", "monitor", "disturbance-transfer" },
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

    [LibraryImport(Lib, EntryPoint = "o103_disturbance")]
    public static partial int Disturbance(IntPtr h, byte[] asdu, int len, int budgetMs);

    [LibraryImport(Lib, EntryPoint = "o103_disturbance_head")]
    public static partial void DisturbanceHead(IntPtr h, int[] head);

    [LibraryImport(Lib, EntryPoint = "o103_disturbance_tags")]
    public static partial int DisturbanceTags(IntPtr h, int i, int[] tags, int max);

    [LibraryImport(Lib, EntryPoint = "o103_disturbance_channel")]
    public static partial int DisturbanceChannel(IntPtr h, int acc, float[] ratings, int[] values, int n);

    [LibraryImport(Lib, EntryPoint = "o103_close")]
    public static partial void Close(IntPtr h);
}

/// <summary>The control system: IEC8705103Manager's calls; each result is the frames its port read.</summary>
internal sealed class Open103Link : ILink103, IDisturbance103
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

    public List<Dictionary<string, object?>> DisturbanceList(int ca, int fun, int timeoutMs, Action<byte[]> received) =>
        throw new NotSentException("IEC8705103Manager has no request of the list");

    // IEC8705103Manager's procedure of clause 7.4.7: an ASDU 23 makes it select
    // the first fault of the list (ASDU 24, TOO 1), and every following ASDU
    // of the transfer its next order or acknowledgement, up to the
    // acknowledgement of the end of the disturbance data (TOO 64). The station
    // sends the list after a general interrogation (clause 7.4.7), so the
    // operation starts with one; the adapter polls and hands each ASDU of
    // types 23 to 31 to Open103.
    public Dictionary<string, object?> DisturbanceTransfer(int ca, int fun, int fan, int timeoutMs, Action<byte[]> received)
    {
        var clock = System.Diagnostics.Stopwatch.StartNew();
        var listed = false;
        var acd = false;
        var done = false;

        void Handle(LinkResponse? resp)
        {
            if (resp is null)
            {
                return;
            }

            acd = resp.Acd;
            foreach (var asdu in (resp.Earlier ?? []).Append(resp.Asdu).OfType<byte[]>())
            {
                received(asdu);
                if (done || asdu.Length < 6 || asdu[0] is < 23 or > 31)
                {
                    continue;
                }

                if (asdu[0] == 23)
                {
                    // Later lists (after the transfer, after another
                    // interrogation) would make Open103 start again.
                    if (listed || asdu.Length < 8)
                    {
                        continue;
                    }

                    listed = true;
                    var first = asdu[6] | (asdu[7] << 8);
                    if (first != fan)
                    {
                        throw new NotSentException($"IEC8705103Manager transfers the first fault of the list, FAN {first}, not {fan}");
                    }
                }
                else if (!listed)
                {
                    continue;
                }

                var rc = Native.Disturbance(_h, asdu, asdu.Length, _budget);
                if (rc < 0)
                {
                    throw new TimeoutException("IEC8705103Manager did not get its order or acknowledgement confirmed");
                }

                done = rc == 1;
                if (Result(rc) is { } confirm)
                {
                    acd = confirm.Acd;
                }
            }
        }

        Handle(Result(Native.GeneralInterrogation(_h, 0, _budget)));
        while (!done && clock.ElapsedMilliseconds < timeoutMs)
        {
            if (!Connected)
            {
                throw new IOException("the station closed the connection");
            }

            Handle(acd ? Class1() : Class2());
            if (!acd && !done)
            {
                Thread.Sleep(20);
            }
        }

        if (!done)
        {
            throw new TimeoutException(listed
                ? "the transfer did not end with the end of the disturbance data acknowledged positively (TOO 32, 64)"
                : "no list of recorded disturbances (ASDU 23) after the general interrogation");
        }

        var head = new int[8];
        Native.DisturbanceHead(_h, head);
        var tags = new List<(int, int, int, int)>();
        for (var i = 0; i < head[7]; i++)
        {
            var buf = new int[1 + 3 * 255];
            var n = Native.DisturbanceTags(_h, i, buf, buf.Length);
            for (var k = 0; k < n; k++)
            {
                tags.Add((buf[0], buf[1 + 3 * k], buf[2 + 3 * k], buf[3 + 3 * k]));
            }
        }

        var channels = new List<(int, double, double, double, IEnumerable<int>)>();
        for (var acc = 1; acc < 255; acc++)
        {
            var ratings = new float[3];
            var values = new int[head[3]];
            if (Native.DisturbanceChannel(_h, acc, ratings, values, values.Length) == acc)
            {
                channels.Add((acc, ratings[0], ratings[1], ratings[2], values));
            }
        }

        return Disturbances.Record(head[0], null, null, head[2], head[3], head[1], Disturbances.Time32(head[4], head[5], head[6]), tags, channels);
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
