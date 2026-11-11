// SPDX-License-Identifier: GPL-3.0-or-later
//
// The client operations of IEC 103 (docs/CONTAINER_CONTRACT.md) over the link
// services of a library. The library frames, numbers (FCB) and encodes; this
// file only decides which service comes next, as a control system's
// application would, and turns what arrives into the result document.

using System.Diagnostics;
using System.Net.Sockets;

namespace Interop103;

/// <summary>What a secondary station answered to one link service.</summary>
/// <param name="Asdu">The ASDU of a user data response, null for a short message.</param>
/// <param name="Acd">Access demand: the station has class 1 data.</param>
/// <param name="Negative">NACK (function 1) or a response that is no confirmation.</param>
/// <param name="Earlier">ASDUs of responses the library read before this one within the same service, in order.</param>
public sealed record LinkResponse(byte[]? Asdu, bool Acd, bool Negative, IReadOnlyList<byte[]>? Earlier = null);

/// <summary>The library would not send the request at all; the message says why.</summary>
public sealed class NotSentException(string message) : Exception(message);

/// <summary>The link services and ASDU builders of one IEC 103 master library.</summary>
public interface ILink103 : IDisposable
{
    /// <summary>Open the TCP connection to the station with this link address; throws SocketException or TimeoutException.</summary>
    /// <param name="serviceTimeoutMs">How long one link service waits for the answer.</param>
    void Open(string host, int port, int connectTimeoutMs, int linkAddress, int serviceTimeoutMs);

    /// <summary>Start the link (reset of the communication unit, as the library does it). Null: no answer.</summary>
    LinkResponse? StartLink();

    LinkResponse? Class1();

    LinkResponse? Class2();

    LinkResponse? GeneralInterrogation(int commonAddress, int scn);

    LinkResponse? ClockSync(int commonAddress, DateTime time);

    LinkResponse? GeneralCommand(int commonAddress, int fun, int inf, int dco, int rii);

    /// <summary>The connection is still open.</summary>
    bool Connected { get; }
}

public sealed class ConnectionLostException(string message) : Exception(message);

public static class ClientRunner
{
    private static readonly string[] Operations =
        ["connect", "interrogate", "clock-sync", "command", "monitor", "generic-read", "generic-write", "disturbance-list", "disturbance-transfer"];

    public static string Usage(string adapter) => $"""
        usage: iec60870-5-interop <command>

          client <operation> --host HOST [--port N] [--common-address N]
                 [--timeout-ms N] [--connect-timeout-ms N] [--collect-ms N] [LINK] [operation flags]
              connect      [--hold-ms N]
              interrogate  [--scn N]
              clock-sync   [--time 2026-01-02T03:04:05.678Z]
              command      --fun N --inf N --dco 1|2|off|on [--rii N]
              monitor      [--duration-ms N] [--max-asdus N]
              generic-read --gin G.E [--kod N]
              generic-write --gin G.E [--kod N] --type 1|3|4|7 --size N --value V
              disturbance-list [--fun N]
              disturbance-transfer --fan N [--fun N]
          print-capabilities
          print-fixture [NAME]
          version

          LINK: [--link-mode unbalanced] [--link-address N]

        See docs/CONTAINER_CONTRACT.md in https://github.com/otfabric/iec60870-5-interop ({adapter})

        """;

    private sealed class Result
    {
        public required string Operation;
        public bool Ok;
        public string? Code;
        public string? Message;
        public bool Connected;
        public readonly List<Dictionary<string, object?>> Confirmations = [];
        public bool Terminated;
        public readonly List<Dictionary<string, object?>> Asdus = [];
        public readonly Stopwatch Clock = Stopwatch.StartNew();
        public Dictionary<string, object?>? Generic;
        public List<Dictionary<string, object?>>? Faults;
        public Dictionary<string, object?>? Disturbance;

        public int Fail(string code, string message, int exit)
        {
            Ok = false;
            Code = code;
            Message = message;
            return exit;
        }

        public void Emit(string adapter)
        {
            var doc = new Dictionary<string, object?>
            {
                ["schemaVersion"] = "1.0", ["adapter"] = adapter, ["operation"] = Operation, ["ok"] = Ok,
                ["error"] = Ok ? null : new Dictionary<string, object?> { ["code"] = Code, ["message"] = Message },
                ["connected"] = Connected, ["confirmations"] = Confirmations, ["terminated"] = Terminated,
                ["asdus"] = Asdus, ["elapsedMs"] = Clock.ElapsedMilliseconds,
            };
            if (Generic is not null)
            {
                doc["generic"] = Generic;
            }

            if (Faults is not null)
            {
                doc["disturbances"] = Faults;
            }

            if (Disturbance is not null)
            {
                doc["disturbance"] = Disturbance;
            }

            Out.Emit(doc);
        }
    }

    /// <summary>Runs "client &lt;operation&gt; ..." and returns the exit code.</summary>
    /// <param name="supports">Which operations and flags the library has: an unsupported one is a usage error.</param>
    public static int Run(string adapter, string op, Args a, Func<ILink103> makeLink, Func<string, string?> unsupported)
    {
        if (!Operations.Contains(op))
        {
            throw new UsageException($"client: unknown or unsupported operation '{op}'");
        }

        var host = a.Str("--host");
        var port = a.Range("--port", 2401, 1, 65535);
        var ca = a.Range("--common-address", 1, 0, 255);
        var timeoutMs = a.Range("--timeout-ms", 5000, 1, 3600000);
        var connectMs = a.Range("--connect-timeout-ms", 5000, 1, 3600000);
        var collectMs = a.Range("--collect-ms", 0, 0, 3600000);
        var linkAddress = a.Link();
        int hold = 0, scn = 0, fun = 0, inf = 0, dco = 0, rii = 1, duration = 1000, maxAsdus = 0;
        var svc = new ServiceArgs();
        var time = DateTime.UtcNow;
        switch (op)
        {
            case "connect":
                hold = a.Range("--hold-ms", 0, 0, 3600000);
                break;
            case "interrogate":
                scn = a.Range("--scn", 0, 0, 255);
                break;
            case "clock-sync":
                if (a.Flag("--broadcast"))
                {
                    throw new UsageException("clock-sync --broadcast: " + (unsupported("broadcast") ?? "not supported"));
                }

                time = AsduDoc.ParseTime(a.Str("--time"));
                break;
            case "command":
                fun = a.Range("--fun", -1, 0, 255);
                inf = a.Range("--inf", -1, 0, 255);
                if (fun < 0 || inf < 0)
                {
                    throw new UsageException("command: --fun and --inf are required");
                }

                dco = AsduDoc.ParseDco(a.Str("--dco"));
                rii = a.Range("--rii", 1, 0, 255);
                break;
            case "monitor":
                duration = a.Range("--duration-ms", 1000, 0, 3600000);
                maxAsdus = a.Range("--max-asdus", 0, 0, 1000000);
                break;
            case "generic-read":
            case "generic-write":
                (svc.Group, svc.Entry) = GenericDoc.ParseGin(a.Str("--gin"));
                svc.Kod = a.Range("--kod", 1, 0, 255);
                if (op == "generic-write")
                {
                    svc.Type = a.Range("--type", -1, 0, 255);
                    svc.Size = a.Range("--size", -1, 1, 255);
                    if (svc.Type < 0 || svc.Size < 0)
                    {
                        throw new UsageException("generic-write: --type and --size are required");
                    }

                    svc.Value = a.Str("--value");
                    _ = GenericDoc.ParseValue(svc.Type, svc.Size, svc.Value);
                }

                break;
            case "disturbance-list":
            case "disturbance-transfer":
                svc.Fun = a.Range("--fun", -1, 0, 255);
                if (op == "disturbance-transfer")
                {
                    svc.Fan = a.Range("--fan", -1, 1, 65535);
                    if (svc.Fan < 0)
                    {
                        throw new UsageException("disturbance-transfer: --fan is required");
                    }
                }

                break;
        }

        a.Finish("client " + op);
        if (host is null)
        {
            throw new UsageException("client: --host is required");
        }

        if (unsupported(op) is { } why)
        {
            throw new UsageException($"client {op}: {why}");
        }

        if (svc.Fun >= 0 && unsupported("fun " + svc.Fun) is { } whyFun)
        {
            throw new UsageException($"client {op} --fun {svc.Fun}: {whyFun}");
        }

        var r = new Result { Operation = op };
        int code;
        using (var link = makeLink())
        {
            code = Execute(r, link, op, host, port, linkAddress, ca, timeoutMs, connectMs, collectMs, hold, scn, time, fun, inf, dco, rii, duration, maxAsdus, svc);
        }

        r.Emit(adapter);
        return code;
    }

    private static int Execute(Result r, ILink103 link, string op, string host, int port, int linkAddress, int ca, int timeoutMs, int connectMs,
        int collectMs, int hold, int scn, DateTime time, int fun, int inf, int dco, int rii, int duration, int maxAsdus, ServiceArgs svc)
    {
        try
        {
            link.Open(host, port, connectMs, linkAddress, Math.Min(timeoutMs, 1000));
        }
        catch (Exception e) when (e is SocketException or TimeoutException or IOException or AggregateException)
        {
            return r.Fail("connect-failed", e.Message, Exit.Connect);
        }

        var acd = false;
        Dictionary<string, object?>? Take(LinkResponse? resp)
        {
            if (resp is null)
            {
                return null;
            }

            acd = resp.Acd;
            foreach (var earlier in resp.Earlier ?? [])
            {
                r.Asdus.Add(AsduDoc.Build(earlier));
            }

            if (resp.Asdu is null)
            {
                return null;
            }

            var d = AsduDoc.Build(resp.Asdu);
            r.Asdus.Add(d);
            return d;
        }

        try
        {
            // ---- link start-up ----
            var deadline = Stopwatch.StartNew();
            LinkResponse? start = null;
            while (deadline.ElapsedMilliseconds < timeoutMs && link.Connected)
            {
                start = link.StartLink();
                if (start is { Negative: false })
                {
                    break;
                }

                start = null;
            }

            if (start is null)
            {
                return link.Connected
                    ? r.Fail("link-init-timeout", "the station did not confirm the reset of the communication unit", Exit.Connect)
                    : r.Fail("connection-lost", "the connection closed during the link start-up", Exit.Connect);
            }

            r.Connected = true;
            Take(start);

            // The class 1 data the reset queued (the identification) comes first.
            for (var i = 0; acd && i < 64; i++)
            {
                Take(link.Class1());
            }

            // ---- the generic services and disturbance data: the library's own procedures ----
            if (op.StartsWith("generic-", StringComparison.Ordinal) || op.StartsWith("disturbance-", StringComparison.Ordinal))
            {
                return Service(r, link, op, ca, timeoutMs, svc);
            }

            // ---- the operation ----
            Func<Dictionary<string, object?>, bool>? done = null;
            var opMs = timeoutMs;
            switch (op)
            {
                case "connect":
                    opMs = hold;
                    break;
                case "monitor":
                {
                    opMs = duration;
                    var n = 0;
                    done = _ => maxAsdus > 0 && ++n >= maxAsdus;
                    break;
                }
                case "interrogate":
                    Take(link.GeneralInterrogation(ca, scn));
                    done = d =>
                    {
                        if ((int)d["typeId"]! == 8 && AsduDoc.Field(d, "scn") == scn)
                        {
                            r.Terminated = true;
                            return true;
                        }

                        return false;
                    };
                    break;
                case "clock-sync":
                    Take(link.ClockSync(ca, time));
                    done = d =>
                    {
                        if ((int)d["typeId"]! == 6 && Convert.ToInt32(d["cot"]) == 8)
                        {
                            r.Confirmations.Add(new() { ["cot"] = 8, ["negative"] = false });
                            return true;
                        }

                        return false;
                    };
                    break;
                case "command":
                    Take(link.GeneralCommand(ca, fun, inf, dco, rii));
                    done = d =>
                    {
                        var t = (int)d["typeId"]!;
                        var cot = Convert.ToInt32(d["cot"]);
                        if (t is 1 or 2 && cot is 20 or 21 && AsduDoc.Field(d, "fun") == fun && AsduDoc.Field(d, "inf") == inf &&
                            AsduDoc.Field(d, "sin") == rii)
                        {
                            r.Confirmations.Add(new() { ["cot"] = cot, ["negative"] = cot == 21 });
                            return true;
                        }

                        return false;
                    };
                    break;
            }

            // An answer can already have come with the confirmation.
            var finished = done is not null && r.Asdus.Any(done);
            var clock = Stopwatch.StartNew();
            while (!finished && clock.ElapsedMilliseconds < opMs)
            {
                if (!link.Connected)
                {
                    throw new ConnectionLostException("the station closed the connection");
                }

                var d = Take(acd ? link.Class1() : link.Class2());
                if (d is not null && done is not null && done(d))
                {
                    finished = true;
                }
                else if (d is null && !acd)
                {
                    Thread.Sleep(20);
                }
            }

            switch (op)
            {
                case "connect":
                case "monitor":
                    if (!link.Connected)
                    {
                        return r.Fail("connection-lost", "the connection closed", Exit.Failed);
                    }

                    break;
                default:
                    if (!finished)
                    {
                        return r.Fail("timeout", "the station did not answer in time", Exit.Failed);
                    }

                    break;
            }

            var refused = op == "command" && r.Confirmations[0]["negative"] is true;
            var collect = Stopwatch.StartNew();
            while (collect.ElapsedMilliseconds < collectMs && link.Connected)
            {
                if (Take(acd ? link.Class1() : link.Class2()) is null && !acd)
                {
                    Thread.Sleep(20);
                }
            }

            if (refused)
            {
                return r.Fail("negative-confirmation", "the station refused the command (cause 21)", Exit.Failed);
            }

            r.Ok = true;
            return Exit.Ok;
        }
        catch (NotSentException e)
        {
            return r.Fail("failed", e.Message, Exit.Failed);
        }
        catch (ConnectionLostException e)
        {
            return r.Fail("connection-lost", e.Message, Exit.Failed);
        }
        catch (Exception e) when (e is IOException or ObjectDisposedException or SocketException)
        {
            return r.Fail("connection-lost", e.Message, Exit.Failed);
        }
    }

    /// <summary>The flags of the generic services and of the transmission of disturbance data.</summary>
    private sealed class ServiceArgs
    {
        public int Group, Entry, Kod = 1, Type = -1, Size = -1, Fun = -1, Fan = -1;
        public string? Value;
    }

    private static int Service(Result r, ILink103 link, string op, int ca, int timeoutMs, ServiceArgs svc)
    {
        void Received(byte[] asdu) => r.Asdus.Add(AsduDoc.Build(asdu));
        try
        {
            switch (op)
            {
                case "generic-read":
                case "generic-write":
                {
                    var g = (IGeneric103)link;
                    var (cot, sets) = op == "generic-read"
                        ? g.GenericRead(ca, svc.Group, svc.Entry, svc.Kod, timeoutMs, Received)
                        : g.GenericWrite(ca, svc.Group, svc.Entry, svc.Kod, svc.Type, svc.Size, svc.Value!, timeoutMs, Received);
                    r.Confirmations.Add(new() { ["cot"] = cot, ["negative"] = false });
                    r.Generic = new() { ["cot"] = cot, ["sets"] = sets };
                    break;
                }
                case "disturbance-list":
                    r.Faults = ((IDisturbance103)link).DisturbanceList(ca, svc.Fun, timeoutMs, Received);
                    break;
                case "disturbance-transfer":
                    r.Disturbance = ((IDisturbance103)link).DisturbanceTransfer(ca, svc.Fun, svc.Fan, timeoutMs, Received);
                    break;
            }
        }
        catch (NegativeAnswerException e)
        {
            r.Confirmations.Add(new() { ["cot"] = e.Cot, ["negative"] = true });
            r.Generic = new() { ["cot"] = e.Cot, ["sets"] = e.Sets };
            return r.Fail("negative-confirmation", e.Message, Exit.Failed);
        }
        catch (TimeoutException e)
        {
            return r.Fail("timeout", e.Message, Exit.Failed);
        }
        catch (NotSentException e)
        {
            return r.Fail("failed", e.Message, Exit.Failed);
        }
        catch (Exception e) when (e is FormatException or InvalidOperationException or ArgumentException or IndexOutOfRangeException)
        {
            // The library gave up on what the station sent.
            return r.Fail("failed", $"{e.GetType().Name}: {e.Message}", Exit.Failed);
        }

        if (!link.Connected)
        {
            return r.Fail("connection-lost", "the station closed the connection", Exit.Failed);
        }

        r.Ok = true;
        return Exit.Ok;
    }

    public static int PrintFixture(string name)
    {
        if (name.IndexOfAny(['/', '\\']) >= 0 || name.Contains(".."))
        {
            Out.Log($"print-fixture: '{name}' is not a fixture name");
            return Exit.Usage;
        }

        var path = Path.Combine("/fixtures", name, "fixture.json");
        if (!File.Exists(path))
        {
            Out.Log($"print-fixture: no fixture named '{name}' in this image");
            return Exit.Failed;
        }

        using var stdout = Console.OpenStandardOutput();
        var bytes = File.ReadAllBytes(path);
        stdout.Write(bytes, 0, bytes.Length);
        return Exit.Ok;
    }

    /// <summary>The features of a capability document, with the IEC 101/104 ones false.</summary>
    public static Dictionary<string, object?> Features(bool gi, bool clockSync, bool command, bool ident, bool measurands, bool broadcast,
        bool generic = false, bool disturbanceList = false, bool disturbanceData = false, bool disturbanceDataServer = false) => new()
    {
        ["generalInterrogation"] = gi, ["groupInterrogation"] = false, ["counterInterrogation"] = false, ["read"] = false,
        ["clockSync"] = clockSync, ["testCommand"] = false, ["directCommands"] = command, ["selectBeforeOperate"] = false,
        ["commandDeactivation"] = false, ["timeTaggedCommands"] = false, ["spontaneousOnCommand"] = false,
        ["apciParameters"] = false, ["multipleConnections"] = false, ["pointQualityOnBitstring"] = false,
        ["tls"] = false, ["tlsServer"] = false, ["tlsClient"] = false, ["dataTransferEvents"] = false,
        ["fileTransfer"] = false, ["fileServer"] = false, ["fileClient"] = false, ["fileUploadServer"] = false,
        ["fileUploadClient"] = false, ["fileDirectoryServer"] = false, ["fileDirectoryClient"] = false,
        ["redundancyServer"] = false, ["redundancyClient"] = false, ["secureAuthentication"] = false,
        ["unbalancedMode"] = true, ["balancedMode"] = false, ["distinctBalancedAddresses"] = false,
        ["generalCommand"] = command, ["identification"] = ident, ["measurands"] = measurands,
        ["broadcastClockSync"] = broadcast, ["genericServices"] = generic, ["disturbanceList"] = disturbanceList,
        ["disturbanceData"] = disturbanceData, ["disturbanceDataServer"] = disturbanceDataServer,
    };
}
