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
                    return ClientRunner.Run(Adapter, argv[1], new Args(argv.Skip(2)), () => new VitusLink(), what => what switch
                    {
                        "broadcast" => "Iec103Client has no SEND/NO REPLY service",
                        "fun 255" => null,
                        // DisturbanceAsdu.DisturbanceCommand writes FUN 255 into every order.
                        _ when what.StartsWith("fun ", StringComparison.Ordinal) => "Iec103Client sends its disturbance orders with function type 255 only",
                        _ => null,
                    });
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
        // disturbanceDataServer: Iec103Server sends samples it generates (a
        // sine, DisturbanceChannel103Model.GenerateSamples) and the station's
        // indications as tags, not the fixture's values and tags.
        ["features"] = ClientRunner.Features(gi: true, clockSync: true, command: true, ident: true, measurands: false, broadcast: false,
            generic: true, disturbanceList: true, disturbanceData: true, disturbanceDataServer: false),
        ["pointTypes"] = Array.Empty<string>(), ["commandTypes"] = Array.Empty<string>(),
        ["asduTypes"] = new Dictionary<string, object?>
        {
            ["monitor"] = new[] { 1, 5, 6, 8, 10, 23, 26, 27, 29, 30, 31 }, ["control"] = new[] { 6, 7, 10, 20, 21, 24, 25 },
        },
        ["clientOperations"] = new[]
        {
            "connect", "interrogate", "clock-sync", "command", "monitor", "generic-read", "generic-write", "disturbance-list",
            "disturbance-transfer",
        },
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

/// <summary>The control system: Iec103Client's synchronous link services, and its
/// procedures of the generic services and of the transmission of disturbance data.</summary>
internal sealed class VitusLink : ILink103, IGeneric103, IDisturbance103
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

    /// <summary>Runs one of Iec103Client's procedures and hands every ASDU it received to received, in order.</summary>
    private T Capture<T>(Action<byte[]> received, List<Asdu103> seen, Func<T> procedure)
    {
        void On(ReceivedAsdu103 r)
        {
            lock (seen)
            {
                seen.Add(r.Asdu);
            }
        }

        _client!.AsduReceived += On;
        try
        {
            return procedure();
        }
        finally
        {
            _client.AsduReceived -= On;
            lock (seen)
            {
                foreach (var a in seen)
                {
                    received(a.Encode());
                }
            }
        }
    }

    private static Dictionary<string, object?> SetDoc(GenericDataSet103 s) =>
        GenericDoc.Set(s.Gin.Group, s.Gin.Entry, s.Kod, s.Gdd.DataType, s.Gdd.DataWidth, s.Gdd.Number, s.Gid);

    // ReadGenericEntries: ASDU 21 with the GIN and KOD, then class 1 until the
    // ASDU 10 with its RII and the continuation bit clear.
    public (int Cot, List<Dictionary<string, object?>> Sets) GenericRead(int ca, int group, int entry, int kod, int timeoutMs, Action<byte[]> received)
    {
        var seen = new List<Asdu103>();
        var sets = Capture(received, seen, () => _client!.ReadGenericEntries(_address, (new Gin103((byte)group, (byte)entry), (byte)kod)));
        var answer = seen.LastOrDefault(a => a.TypeId == TypeId103.GenericData && a.Cot is Cot103.GenericReadValid or Cot103.GenericReadInvalid);
        if (answer is null)
        {
            throw new TimeoutException("Iec103Client.ReadGenericEntries returned without an ASDU 10 with cause 42 or 43");
        }

        var docs = sets.Select(SetDoc).ToList();
        if (answer.Cot == Cot103.GenericReadInvalid)
        {
            throw new NegativeAnswerException(answer.Cot, docs, "the station answered the generic read with cause 43");
        }

        return (answer.Cot, docs);
    }

    // WriteGenericValue: ASDU 10, cause 40, one data set, then class 1 until
    // the ASDU 10 with cause 40 or 41.
    public (int Cot, List<Dictionary<string, object?>> Sets) GenericWrite(int ca, int group, int entry, int kod, int type, int size, string value,
        int timeoutMs, Action<byte[]> received)
    {
        var gin = new Gin103((byte)group, (byte)entry);
        var v = GenericDoc.ParseValue(type, size, value);
        var set = type switch
        {
            1 => GenericDataSet103.Ascii(gin, (byte)kod, (string)v),
            3 => GenericDataSet103.Unsigned(gin, (byte)kod, (uint)(long)v, (byte)size),
            4 => GenericDataSet103.Signed(gin, (byte)kod, (int)(long)v, (byte)size),
            _ => GenericDataSet103.Float(gin, (byte)kod, (float)(double)v),
        };
        var seen = new List<Asdu103>();
        var ack = Capture(received, seen, () => _client!.WriteGenericValue(_address, set));
        var answer = seen.Last(a => a.TypeId == TypeId103.GenericData && a.Cot is Cot103.GenericWriteAckPositive or Cot103.GenericWriteAckNegative);
        var docs = new List<Dictionary<string, object?>> { SetDoc(ack) };
        if (answer.Cot == Cot103.GenericWriteAckNegative)
        {
            throw new NegativeAnswerException(answer.Cot, docs, "the station answered the generic write with cause 41");
        }

        return (answer.Cot, docs);
    }

    // RequestDisturbanceList: ASDU 24 with TOO 24, then class 1 until no data.
    public List<Dictionary<string, object?>> DisturbanceList(int ca, int fun, int timeoutMs, Action<byte[]> received)
    {
        var seen = new List<Asdu103>();
        var entries = Capture(received, seen, () => _client!.RequestDisturbanceList(_address));
        if (!seen.Any(a => a.TypeId == TypeId103.DisturbanceList))
        {
            throw new TimeoutException("Iec103Client.RequestDisturbanceList returned without an ASDU 23");
        }

        return entries.Select(e => Disturbances.Fault(e.Fan, e.Sof.TripProtection, e.Sof.TripCommand, e.Sof.Test, e.Sof.EquipmentStart,
            e.FaultTime.ToString("yyyy-MM-dd'T'HH:mm:ss.fff'Z'", System.Globalization.CultureInfo.InvariantCulture))).ToList();
    }

    // ReadDisturbance (selection, ready, its acknowledgement, the channels)
    // and then ReadDisturbanceTags, as Iec103Client offers them.
    public Dictionary<string, object?> DisturbanceTransfer(int ca, int fun, int fan, int timeoutMs, Action<byte[]> received)
    {
        var seen = new List<Asdu103>();
        var (readout, tags) = Capture(received, seen, () =>
        {
            var r = _client!.ReadDisturbance(_address, fan);
            return (r, _client.ReadDisturbanceTags(_address, fan));
        });
        var t = readout.FaultTime;
        return Disturbances.Record(readout.Fan, null, null, readout.ChannelCount, readout.SampleCount, readout.IntervalUs,
            Disturbances.Time32(t.Second * 1000 + t.Millisecond, t.Minute, t.Hour),
            tags.Select(x => (-1, (int)x.Fun, (int)x.Inf, x.Dpi)),
            readout.Channels.Select(c => ((int)c.Acc, (double)c.Primary, (double)c.Secondary, (double)c.Factor, c.Samples.Select(v => (int)v))));
    }
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
            GenericEntries = fx.Generic.Select(g => new GenericEntry103Model
            {
                Group = g.Group, Entry = g.Entry, Description = g.Description, ReadOnly = !g.Writable,
                DataType = g.Type switch { 1 => "ascii", 3 => "uint", 4 => "int", _ => "float" },
                AsciiValue = g.Value as string, ActualValue = g.Value is string ? null : Convert.ToDouble(g.Value),
                IntWidth = g.Type is 3 or 4 ? g.Size : 4,
            }).ToList(),
            // The list (ASDU 23) and the description of each record (ASDUs 26
            // and 27) are the fixture's; the values and tags are not (see
            // print-capabilities: disturbanceDataServer).
            Disturbances = fx.Disturbances.Select(d => new DisturbanceRecord103Model
            {
                Fan = d.Fan, Sof = new Sof103(d.Tp, d.Tm, d.Test, d.Otev), FaultTime = d.Time, SampleIntervalUs = d.Interval,
                Channels = d.Channels.Select(c => new DisturbanceChannel103Model
                {
                    Acc = c.Acc, Primary = c.Rpv, Secondary = c.Rsv, Factor = c.Rfa, SampleCount = c.Values.Count,
                }).ToList(),
            }).ToList(),
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
