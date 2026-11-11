// SPDX-License-Identifier: GPL-3.0-or-later
//
// iec60870-5-interop IEC 60870-5-103 adapter for vitusdong/iec103-dotnet:
// the protection device (Iec103Server, one session per TCP connection) and
// the control system (Iec103Client). It implements the container contract in
// docs/CONTAINER_CONTRACT.md.

using System.Net;
using System.Net.Sockets;
using System.Reflection;
using System.Runtime.InteropServices;
using Iec103;
using Interop103;

internal static class Program
{
    private const string Adapter = "vitusdong";

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
                    return Server.Run(new Args(argv.Skip(1)));
                case "client" when argv.Length >= 2:
                    return ClientRunner.Run(Adapter, argv[1], new Args(argv.Skip(2)), () => new VitusLink(),
                        what => what == "broadcast" ? "Iec103Client has no SEND/NO REPLY service" : null);
                case "print-capabilities":
                    PrintCapabilities();
                    return Exit.Ok;
                case "print-fixture":
                    return ClientRunner.PrintFixture(argv.Length > 1 ? argv[1] : "baseline");
                case "version":
                    Console.WriteLine($"{Adapter} {Versions.Adapter} (vitusdong/iec103-dotnet {Versions.Upstream}, IEC 60870-5-103)");
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
            ["name"] = "vitusdong/iec103-dotnet", ["version"] = Versions.Upstream, ["revision"] = Versions.Revision,
            ["license"] = "MIT", ["language"] = "C#",
            // Its tests use a copy of ArIEC103 as the oracle: the pairing with
            // the ariec103 image is not independent evidence on its own.
            ["independentEngine"] = true,
        },
        ["roles"] = new Dictionary<string, object?> { ["server"] = true, ["client"] = true },
        // measurands: the server sends ASDU 9 with one raw 16-bit value only,
        // so the fixture's measurands are not given to it.
        ["features"] = ClientRunner.Features(gi: true, clockSync: true, command: true, ident: true, measurands: false, broadcast: false),
        ["pointTypes"] = Array.Empty<string>(), ["commandTypes"] = Array.Empty<string>(),
        ["asduTypes"] = new Dictionary<string, object?>
        {
            ["monitor"] = new[] { 1, 5, 6, 8 }, ["control"] = new[] { 6, 7, 20 },
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

/// <summary>The control system: Iec103Client's synchronous link services.</summary>
internal sealed class VitusLink : ILink103
{
    private Iec103Client? _client;
    private volatile bool _closed;
    private byte _address;

    public bool Connected => _client is not null && !_closed;

    public void Open(string host, int port, int connectTimeoutMs, int linkAddress, int serviceTimeoutMs)
    {
        _address = (byte)linkAddress;
        _client = new Iec103Client(host, port) { TimeoutMs = serviceTimeoutMs };
        _client.LinkClosed += () => _closed = true;
        _client.FrameReceived += (hex, _) => Out.Log("rx " + hex);
        _client.Connect(connectTimeoutMs);
    }

    private static LinkResponse? Map(Func<Frame103> service)
    {
        try
        {
            var f = service();
            var ctrl = LinkControl103.FromByte(f.Control);
            return new LinkResponse(f.Format == Frame103Format.VariableLength ? f.Asdu : null, ctrl.FcbOrAcd,
                f.Format != Frame103Format.SingleChar && ctrl.Function == (byte)SecondaryFunction103.Nack);
        }
        catch (TimeoutException)
        {
            return null;
        }
    }

    public LinkResponse? StartLink() => Map(() => _client!.ResetLink(_address));
    public LinkResponse? Class1() => Map(() => _client!.RequestClass1(_address));
    public LinkResponse? Class2() => Map(() => _client!.RequestClass2(_address));

    public LinkResponse? GeneralInterrogation(int ca, int scn) =>
        Map(() => _client!.SendConfirmed(_address, Asdu103.GeneralInterrogation((byte)ca, (byte)scn)));

    public LinkResponse? ClockSync(int ca, DateTime time) =>
        Map(() => _client!.SendConfirmed(_address, Asdu103.ClockSync(Cp56Time2a103.FromDateTime(time), (byte)ca)));

    public LinkResponse? GeneralCommand(int ca, int fun, int inf, int dco, int rii) =>
        Map(() => _client!.SendConfirmed(_address, Asdu103.GeneralCommand((byte)ca, (byte)fun, (byte)inf, dco, (byte)rii)));

    public void Dispose() => _client?.Dispose();
}

/// <summary>The protection device: one Iec103Server session per TCP connection, one at a time.</summary>
internal static class Server
{
    public static int Run(Args a)
    {
        var fixturePath = a.Str("--fixture") ?? "/fixtures/baseline/fixture.json";
        var bind = a.Str("--bind-address") ?? "0.0.0.0";
        var port = a.Range("--port", 2401, 1, 65535);
        var readyFile = a.Str("--ready-file") ?? "/run/iec60870-5-interop/ready";
        var linkAddress = a.Link();
        var caFlag = a.Range("--common-address", -1, 0, 254);
        a.Finish("server");
        if (!IPAddress.TryParse(bind, out var bindIp))
        {
            throw new UsageException($"--bind-address: '{bind}' is not an address");
        }

        Fixture103 fx;
        try
        {
            fx = Fixture103.Load(fixturePath);
        }
        catch (FixtureException e)
        {
            Out.Log(e.Message);
            return Exit.Fixture;
        }

        var ca = caFlag >= 0 ? caFlag : fx.CommonAddress;
        // Iec103StationModel.Address is the link address and the common
        // address of every ASDU at once, 1..247.
        if (ca != linkAddress || linkAddress is < 1 or > 247)
        {
            throw new UsageException($"vitusdong/iec103-dotnet uses one address, 1..247, as link address and common address: " +
                $"link address {linkAddress}, common address {ca}");
        }

        var station = new Iec103StationModel
        {
            Address = linkAddress,
            Name = fx.Name,
            CompatIdent = fx.Ascii,
            PrivateIdent = fx.Ascii,
            // Iec103StationModel has no general-interrogation flag: an
            // indication outside the interrogation is left out.
            Points = fx.Indications.Where(i => i.Gi).Select(i => new Iec103PointModel { Fun = i.Fun, Inf = i.Inf, Dpi = i.Dpi }).ToList(),
        };
        var workspace = new Workspace103Model { Port = port, BindAddress = bind, Stations = [station] };

        var listener = new TcpListener(bindIp, port);
        try
        {
            listener.Start();
        }
        catch (SocketException e)
        {
            Out.Log($"cannot listen on {bind}:{port}: {e.Message}");
            return Exit.Failed;
        }

        using var stop = new CancellationTokenSource();
        using var term = PosixSignalRegistration.Create(PosixSignal.SIGTERM, c => { c.Cancel = true; stop.Cancel(); });
        using var intr = PosixSignalRegistration.Create(PosixSignal.SIGINT, c => { c.Cancel = true; stop.Cancel(); });

        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(readyFile)!);
            File.WriteAllText(readyFile, "ready\n");
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            Out.Log($"cannot write the ready file {readyFile}: {e.Message}");
            return Exit.Failed;
        }

        Out.Emit(new Dictionary<string, object?>
        {
            ["event"] = "ready", ["adapter"] = "vitusdong", ["address"] = $"{bind}:{port}", ["fixture"] = fx.Name,
            ["commonAddress"] = ca, ["standard"] = "iec60870-5-103", ["mode"] = "unbalanced", ["linkAddress"] = linkAddress,
        });

        var busy = 0;
        _ = Task.Run(async () =>
        {
            while (!stop.IsCancellationRequested)
            {
                TcpClient tcp;
                try
                {
                    tcp = await listener.AcceptTcpClientAsync(stop.Token);
                }
                catch (Exception)
                {
                    return;
                }

                var peer = tcp.Client.RemoteEndPoint?.ToString() ?? "?";
                // One line, one control system: another connection is closed at once.
                if (Interlocked.CompareExchange(ref busy, 1, 0) != 0)
                {
                    tcp.Close();
                    continue;
                }

                Out.Emit(new Dictionary<string, object?> { ["event"] = "connection-opened", ["peer"] = peer });
                var session = new Iec103Server(workspace, port, bind);
                var events = new Events(station);
                session.Log += m => Out.Log(m);
                session.FrameTraced += events.Frame;
                session.ClientDisconnected += _ =>
                {
                    Out.Emit(new Dictionary<string, object?> { ["event"] = "connection-closed", ["peer"] = peer });
                    Interlocked.Exchange(ref busy, 0);
                };
                session.StartStream(tcp.GetStream(), tcp, peer);
            }
        });

        stop.Token.WaitHandle.WaitOne();
        try
        {
            File.Delete(readyFile);
        }
        catch (IOException)
        {
        }

        listener.Stop();
        Out.Emit(new Dictionary<string, object?> { ["event"] = "stopped" });
        return Exit.Ok;
    }

    /// <summary>Server events from the frames the session received.</summary>
    private sealed class Events(Iec103StationModel station)
    {
        private readonly FrameStreamParser103 _parser = new();

        public void Frame(FrameTrace trace)
        {
            if (trace.Sent)
            {
                return;
            }

            foreach (var frame in _parser.Feed(Convert.FromHexString(trace.Hex)))
            {
                if (frame.Format != Frame103Format.VariableLength || !Asdu103.TryDecode(frame.Asdu, out var asdu, out _) || asdu is null)
                {
                    continue;
                }

                var known = frame.Address == station.Address;
                switch (asdu.TypeId)
                {
                    case TypeId103.GeneralInterrogation:
                        Out.Emit(new Dictionary<string, object?>
                        {
                            ["event"] = "interrogation", ["commonAddress"] = (int)asdu.Ca,
                            ["scn"] = asdu.Data.Length > 0 ? asdu.Data[0] : 0,
                            ["accepted"] = known && asdu.Fun == Asdu103.GlobalFun && asdu.Inf == 0,
                        });
                        break;
                    case TypeId103.ClockSync when asdu.Data.Length >= 7:
                        Out.Emit(new Dictionary<string, object?>
                        {
                            ["event"] = "clock-sync", ["time"] = AsduDoc.Time56(asdu.Data).Text, ["broadcast"] = frame.Address == 255,
                        });
                        break;
                    case TypeId103.GeneralCommand when known && asdu.Data.Length >= 1 &&
                                                     asdu.Cot is Cot103.CommandAck or Cot103.RemoteOperation or Cot103.LocalOperation:
                    {
                        // Iec103Server's rule: a point with this FUN/INF and a DCT other than 3 is set.
                        var (dct, _, rii) = Asdu103.DecodeGeneralCommand(asdu);
                        var point = station.Points.Any(p => p.Fun == asdu.Fun && p.Inf == asdu.Inf);
                        Out.Emit(new Dictionary<string, object?>
                        {
                            ["event"] = "command", ["fun"] = (int)asdu.Fun, ["inf"] = (int)asdu.Inf, ["dco"] = (int)asdu.Data[0],
                            ["rii"] = (int)rii, ["outcome"] = point && dct != 3 ? "executed" : "rejected",
                        });
                        break;
                    }
                }
            }
        }
    }
}
