// SPDX-License-Identifier: GPL-3.0-or-later
//
// iec60870-5-interop IEC 60870-5-103 adapter for masarray/ARIEC103: the
// control system, ArIEC103's Iec103MasterSession, on a TCP stream. ArIEC103
// reaches its relay through System.IO.Ports only, and SerialPort fails on a
// pseudo terminal (RtsEnable: "Inappropriate ioctl for device"), so the
// driver gives the session its own IByteTransport over the TCP connection:
// ArIEC103's public transport interface, the byte stream unchanged.
//
// The slave simulator of ArIEC103 has a model of its own (fixed points,
// random faults, animated currents) and cannot serve a fixture: the image is
// a client only. It implements the container contract in
// docs/CONTAINER_CONTRACT.md.

using System.Net.Sockets;
using System.Reflection;
using ArIEC103.Master;
using ArIEC103.Master.Model;
using ArIEC103.Master.Protocol;
using ArIEC103.Master.Transport;
using Interop103;

internal static class Program
{
    private const string Adapter = "ariec103";

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
                    throw new UsageException("server: this image has no server (ArIEC103's slave simulator cannot serve a fixture)");
                case "client" when argv.Length >= 2:
                    return ClientRunner.Run(Adapter, argv[1], new Args(argv.Skip(2)), () => new AriLink(),
                        what => what switch
                        {
                            "broadcast" => "Iec103MasterSession has no SEND/NO REPLY service",
                            // Iec103AsduBuilder builds ASDUs 6, 7 and 20 only; AsduDecoder names type 23 and decodes none of 10, 11, 23..31.
                            "generic-read" or "generic-write" => "ARIEC103 has no generic services (Iec103AsduBuilder: ASDUs 6, 7, 20 only)",
                            "disturbance-list" or "disturbance-transfer" => "ARIEC103 has no transmission of disturbance data (Iec103AsduBuilder: ASDUs 6, 7, 20 only)",
                            _ => null,
                        });
                case "print-capabilities":
                    PrintCapabilities();
                    return Exit.Ok;
                case "print-fixture":
                    return ClientRunner.PrintFixture(argv.Length > 1 ? argv[1] : "baseline");
                case "version":
                    Console.WriteLine($"{Adapter} {Versions.Adapter} (masarray/ARIEC103 {Versions.Upstream}, IEC 60870-5-103)");
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
            ["name"] = "masarray/ARIEC103", ["version"] = Versions.Upstream, ["revision"] = Versions.Revision,
            ["license"] = "Apache-2.0", ["language"] = "C#", ["independentEngine"] = true,
        },
        ["roles"] = new Dictionary<string, object?> { ["server"] = false, ["client"] = true },
        ["features"] = ClientRunner.Features(gi: true, clockSync: true, command: true, ident: true, measurands: true, broadcast: false),
        ["pointTypes"] = Array.Empty<string>(), ["commandTypes"] = Array.Empty<string>(),
        ["asduTypes"] = new Dictionary<string, object?>
        {
            ["monitor"] = new[] { 1, 2, 5, 6, 7, 8, 9, 20, 23 }, ["control"] = new[] { 6, 7, 20 },
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

/// <summary>ArIEC103's IByteTransport over the TCP connection.</summary>
internal sealed class TcpByteTransport(string host, int port, int connectTimeoutMs) : IByteTransport
{
    private TcpClient? _tcp;
    private NetworkStream? _stream;

    public bool IsOpen => _tcp?.Connected == true && !Closed;

    public volatile bool Closed;

    public async ValueTask OpenAsync(CancellationToken cancellationToken)
    {
        _tcp = new TcpClient { NoDelay = true };
        using var cts = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        cts.CancelAfter(connectTimeoutMs);
        try
        {
            await _tcp.ConnectAsync(host, port, cts.Token).ConfigureAwait(false);
        }
        catch (OperationCanceledException)
        {
            throw new TimeoutException($"no TCP connection to {host}:{port} within {connectTimeoutMs} ms");
        }

        _stream = _tcp.GetStream();
    }

    public ValueTask CloseAsync(CancellationToken cancellationToken)
    {
        Closed = true;
        _tcp?.Close();
        return ValueTask.CompletedTask;
    }

    public async ValueTask WriteAsync(ReadOnlyMemory<byte> buffer, CancellationToken cancellationToken)
    {
        Out.Log("tx " + Convert.ToHexString(buffer.Span));
        await _stream!.WriteAsync(buffer, cancellationToken).ConfigureAwait(false);
    }

    public async ValueTask<int> ReadAsync(Memory<byte> buffer, CancellationToken cancellationToken)
    {
        var n = await _stream!.ReadAsync(buffer, cancellationToken).ConfigureAwait(false);
        if (n == 0)
        {
            Closed = true;
        }

        return n;
    }

    public ValueTask DisposeAsync() => CloseAsync(CancellationToken.None);

    public void Dispose() => CloseAsync(CancellationToken.None);
}

/// <summary>The control system: the link services of Iec103MasterSession.</summary>
internal sealed class AriLink : ILink103
{
    // Iec103MasterSession has public services for the link, the polls, the
    // interrogation with scan number 0 and the clock synchronization with the
    // current time; its variable-frame service is private. The ASDUs the
    // contract needs (another scan number, a given time, a general command)
    // are built by its public Iec103AsduBuilder and sent through that
    // service, so FCB handling and framing stay ArIEC103's.
    private static readonly MethodInfo SendVariable = typeof(Iec103MasterSession).GetMethod(
        "SendVariableAndReceiveAsync", BindingFlags.Instance | BindingFlags.NonPublic)
        ?? throw new MissingMethodException("Iec103MasterSession.SendVariableAndReceiveAsync");

    private TcpByteTransport? _transport;
    private Iec103MasterSession? _session;
    private readonly List<Iec103MasterEvidenceEvent> _rx = [];

    public bool Connected => _transport is { Closed: false };

    public void Open(string host, int port, int connectTimeoutMs, int linkAddress, int serviceTimeoutMs)
    {
        var settings = new Iec103MasterSettings
        {
            PortName = $"tcp://{host}:{port}",
            LinkAddress = (byte)linkAddress,
            CommonAddress = (byte)linkAddress,
            ResponseTimeoutMs = serviceTimeoutMs,
            TargetProfile = "iec60870-5-interop station",
        };
        _transport = new TcpByteTransport(host, port, connectTimeoutMs);
        try
        {
            _transport.OpenAsync(CancellationToken.None).AsTask().GetAwaiter().GetResult();
        }
        catch (SocketException e)
        {
            throw new IOException(e.Message, e);
        }

        _session = new Iec103MasterSession(settings, _transport);
        _session.EvidenceReceived += (_, e) =>
        {
            if (e.Direction == ArIEC103.Core.Model.FrameDirection.SlaveToMaster && e.Frame is not null)
            {
                lock (_rx)
                {
                    _rx.Add(e);
                }
            }
        };
    }

    private LinkResponse? Service(Func<Task> call)
    {
        lock (_rx)
        {
            _rx.Clear();
        }

        call().GetAwaiter().GetResult();
        Iec103MasterEvidenceEvent? last;
        lock (_rx)
        {
            last = _rx.LastOrDefault();
        }

        var frame = last?.Frame;
        if (frame is null)
        {
            return null;
        }

        Out.Log("rx " + last!.RawHex);
        var link = frame.LinkControl;
        var asdu = frame.AsduBytes.Count > 0 ? frame.AsduBytes.ToArray() : null;
        return new LinkResponse(asdu, link?.Acd == true, link is { IsSecondaryNack: true });
    }

    private Task Send(string summary, IReadOnlyList<byte> asdu) =>
        (Task)SendVariable.Invoke(_session, [summary, "Class 2", asdu, "iec60870-5-interop", CancellationToken.None])!;

    public LinkResponse? StartLink()
    {
        // ArIEC103's start-up with ResetRemoteLinkOnConnect: reset of the
        // remote link (function 0), then its reset of the FCB (function 7).
        var reset = Service(() => _session!.ResetRemoteLinkAsync(CancellationToken.None));
        if (reset is null || reset.Negative)
        {
            return reset;
        }

        var fcb = Service(() => _session!.ResetFcbAsync("Startup synchronization", CancellationToken.None));
        return fcb is null ? reset : reset with { Acd = reset.Acd || fcb.Acd };
    }

    public LinkResponse? Class1() => Service(() => _session!.RequestClass1Async("iec60870-5-interop", CancellationToken.None));

    public LinkResponse? Class2() => Service(() => _session!.RequestClass2Async("iec60870-5-interop", CancellationToken.None));

    public LinkResponse? GeneralInterrogation(int ca, int scn) =>
        Service(() => Send("General Interrogation", Iec103AsduBuilder.GeneralInterrogation((byte)ca, (byte)scn)));

    public LinkResponse? ClockSync(int ca, DateTime time) =>
        Service(() => Send("Clock Sync", Iec103AsduBuilder.ClockSynchronization((byte)ca, time)));

    // Iec103AsduBuilder.GeneralCommand has no return information identifier:
    // the ASDU ends after the DCO, whatever --rii says.
    public LinkResponse? GeneralCommand(int ca, int fun, int inf, int dco, int rii) =>
        Service(() => Send("General Command", Iec103AsduBuilder.GeneralCommand((byte)ca, (byte)fun, (byte)inf, (byte)dco)));

    public void Dispose() => _transport?.Dispose();
}
