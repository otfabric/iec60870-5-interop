// SPDX-License-Identifier: GPL-3.0-or-later
//
// The IEC 60870-5-103 container contract (docs/CONTAINER_CONTRACT.md) for the
// .NET adapters: arguments, exit codes, JSON output, ASDU documents and the
// fixture. Shared by adapters/iec103/vitusdong and adapters/iec103/ariec103;
// nothing here speaks the protocol, which is each library's.

using System.Globalization;
using System.Text.Encodings.Web;
using System.Text.Json;

namespace Interop103;

public static class Exit
{
    public const int Ok = 0, Failed = 1, Usage = 2, Connect = 3, Fixture = 4;
}

public sealed class UsageException(string message) : Exception(message);

public sealed class FixtureException(string message) : Exception(message);

/// <summary>Command-line options "--name value" and bare "--flag".</summary>
public sealed class Args(IEnumerable<string> rest)
{
    private readonly List<string> _rest = rest.ToList();

    public string? Str(string name)
    {
        var i = _rest.IndexOf(name);
        if (i < 0 || i + 1 >= _rest.Count)
        {
            return null;
        }

        var v = _rest[i + 1];
        _rest.RemoveRange(i, 2);
        return v;
    }

    public int Range(string name, int def, int lo, int hi)
    {
        var s = Str(name);
        if (s is null)
        {
            return def;
        }

        if (!int.TryParse(s, NumberStyles.AllowLeadingSign, CultureInfo.InvariantCulture, out var n))
        {
            throw new UsageException($"{name}: '{s}' is not an integer");
        }

        if (n < lo || n > hi)
        {
            throw new UsageException($"{name}: must be {lo}..{hi}, got {n}");
        }

        return n;
    }

    public bool Flag(string name) => _rest.Remove(name);

    public void Finish(string context)
    {
        if (_rest.Count > 0)
        {
            throw new UsageException($"{context}: unknown argument {_rest[0]}");
        }
    }

    /// <summary>The link flags of IEC 103: unbalanced only, one-octet link address.</summary>
    public int Link()
    {
        var mode = Str("--link-mode") ?? "unbalanced";
        if (mode != "unbalanced")
        {
            throw new UsageException($"--link-mode: IEC 103 is unbalanced only, got '{mode}'");
        }

        return Range("--link-address", 1, 0, 254);
    }
}

public static class Out
{
    private static readonly object Lock = new();

    private static readonly JsonSerializerOptions Options = new()
    {
        Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
    };

    public static string Adapter { get; set; } = "?";

    /// <summary>One JSON document on one line of stdout.</summary>
    public static void Emit(object doc)
    {
        var line = JsonSerializer.Serialize(doc, Options);
        lock (Lock)
        {
            Console.Out.Write(line + "\n");
            Console.Out.Flush();
        }
    }

    public static void Log(string message) => Console.Error.WriteLine($"[{Adapter}] {message}");
}

/// <summary>ASDU documents of IEC 103 (docs/CONTAINER_CONTRACT.md).</summary>
public static class AsduDoc
{
    private static readonly Dictionary<int, string> Names = new()
    {
        [1] = "time-tagged-message", [2] = "time-tagged-message-relative", [3] = "measurands-i",
        [4] = "time-tagged-measurands", [5] = "identification", [6] = "time-synchronization",
        [7] = "general-interrogation", [8] = "general-interrogation-termination", [9] = "measurands-ii",
        [10] = "generic-data", [11] = "generic-identification", [20] = "general-command", [21] = "generic-command",
        [23] = "disturbance-list", [24] = "disturbance-order", [25] = "disturbance-acknowledgement",
        [26] = "disturbance-ready", [27] = "channel-ready", [28] = "tags-ready", [29] = "tags",
        [30] = "disturbance-values", [31] = "disturbance-end",
    };

    /// <summary>Length of the elements after FUN and INF that the standard lays out; -1 when not decoded.</summary>
    private static int ElementLength(int type, int count) => type switch
    {
        1 => 6, 2 => 10, 3 or 9 => 2 * count, 4 => 12, 5 => 13, 6 => 7, 7 or 8 => 1, 20 => 2, _ => -1,
    };

    public static string Hex(ReadOnlySpan<byte> b) => Convert.ToHexString(b).ToLowerInvariant();

    private static (string Text, bool Invalid) Time32(ReadOnlySpan<byte> b)
    {
        var ms = b[0] | (b[1] << 8);
        return ($"{b[3] & 0x1f:D2}:{b[2] & 0x3f:D2}:{ms / 1000:D2}.{ms % 1000:D3}", (b[2] & 0x80) != 0);
    }

    public static (string Text, bool Invalid) Time56(ReadOnlySpan<byte> b)
    {
        var ms = b[0] | (b[1] << 8);
        var text = $"{2000 + (b[6] & 0x7f):D4}-{b[5] & 0x0f:D2}-{b[4] & 0x1f:D2}T{b[3] & 0x1f:D2}:{b[2] & 0x3f:D2}:{ms / 1000:D2}.{ms % 1000:D3}Z";
        return (text, (b[2] & 0x80) != 0);
    }

    public static byte[] Cp56(DateTime t)
    {
        var ms = t.Second * 1000 + t.Millisecond;
        return [(byte)(ms & 0xff), (byte)(ms >> 8), (byte)t.Minute, (byte)t.Hour,
            (byte)(t.Day | ((((int)t.DayOfWeek + 6) % 7 + 1) << 5)), (byte)t.Month, (byte)(t.Year % 100)];
    }

    /// <summary>The document of one ASDU: TI, VSQ, COT, CA, FUN, INF and the elements, as on the wire.</summary>
    public static Dictionary<string, object?> Build(ReadOnlySpan<byte> asdu)
    {
        var doc = new Dictionary<string, object?>();
        int type = asdu.Length > 0 ? asdu[0] : 0;
        doc["type"] = Names.TryGetValue(type, out var name) ? name : null;
        doc["typeId"] = type;
        doc["cot"] = asdu.Length > 2 ? asdu[2] : 0;
        doc["negative"] = false;
        doc["test"] = false;
        doc["originator"] = 0;
        doc["commonAddress"] = asdu.Length > 3 ? asdu[3] : 0;
        doc["sequence"] = asdu.Length > 1 && (asdu[1] & 0x80) != 0;
        var count = asdu.Length > 1 ? asdu[1] & 0x7f : 0;
        doc["count"] = count;
        var objects = new List<Dictionary<string, object?>>();
        doc["objects"] = objects;
        if (asdu.Length < 6)
        {
            return doc;
        }

        var obj = new Dictionary<string, object?> { ["fun"] = (int)asdu[4], ["inf"] = (int)asdu[5] };
        objects.Add(obj);
        var el = asdu[6..];
        if (el.Length != ElementLength(type, count))
        {
            obj["data"] = Hex(el);
            return doc;
        }

        switch (type)
        {
            case 1:
            case 2:
            {
                obj["dpi"] = el[0] & 0x03;
                var t = el[1..5];
                if (type == 2)
                {
                    obj["ret"] = el[1] | (el[2] << 8);
                    obj["fan"] = el[3] | (el[4] << 8);
                    t = el[5..9];
                }

                var (text, invalid) = Time32(t);
                obj["time32"] = text;
                if (invalid)
                {
                    obj["timeInvalid"] = true;
                }

                obj["sin"] = (int)el[^1];
                break;
            }
            case 3:
            case 9:
            {
                var values = new List<Dictionary<string, object?>>();
                for (var i = 0; i < count; i++)
                {
                    var u = (ushort)(el[2 * i] | (el[2 * i + 1] << 8));
                    values.Add(new Dictionary<string, object?>
                    {
                        ["value"] = (short)u >> 3, ["ov"] = (u & 1) != 0, ["er"] = (u & 2) != 0,
                    });
                }

                obj["values"] = values;
                break;
            }
            case 4:
            {
                obj["scl"] = (double)BitConverter.ToSingle(el[..4]);
                obj["ret"] = el[4] | (el[5] << 8);
                obj["fan"] = el[6] | (el[7] << 8);
                var (text, invalid) = Time32(el[8..12]);
                obj["time32"] = text;
                if (invalid)
                {
                    obj["timeInvalid"] = true;
                }

                break;
            }
            case 5:
                obj["col"] = (int)el[0];
                obj["ascii"] = System.Text.Encoding.ASCII.GetString(el[1..9]).TrimEnd('\0', ' ');
                obj["software"] = Hex(el[9..13]);
                break;
            case 6:
            {
                var (text, invalid) = Time56(el);
                obj["time"] = text;
                if (invalid)
                {
                    obj["timeInvalid"] = true;
                }

                break;
            }
            case 7:
            case 8:
                obj["scn"] = (int)el[0];
                break;
            case 20:
                obj["dco"] = (int)el[0];
                obj["rii"] = (int)el[1];
                break;
        }

        return doc;
    }

    public static int Field(Dictionary<string, object?> doc, string name)
    {
        if (doc["objects"] is List<Dictionary<string, object?>> { Count: > 0 } objs && objs[0].TryGetValue(name, out var v) && v is int i)
        {
            return i;
        }

        return -1;
    }

    public static DateTime ParseTime(string? s)
    {
        if (s is null)
        {
            return DateTime.UtcNow;
        }

        if (!DateTime.TryParseExact(s, "yyyy-MM-dd'T'HH:mm:ss.fff'Z'", CultureInfo.InvariantCulture,
                DateTimeStyles.AdjustToUniversal | DateTimeStyles.AssumeUniversal, out var t))
        {
            throw new UsageException($"--time: '{s}' is not like 2026-01-02T03:04:05.678Z");
        }

        return t;
    }

    public static int ParseDco(string? s) => s?.ToLowerInvariant() switch
    {
        null => throw new UsageException("command: --dco is required"),
        "off" => 1,
        "on" => 2,
        _ when int.TryParse(s, out var n) && n is >= 0 and <= 3 => n,
        _ => throw new UsageException($"--dco: must be 0..3, off or on, got '{s}'"),
    };
}

/// <summary>A fixture of IEC 103 (fixtures/schema/iec60870-5-103-fixture.schema.json).</summary>
public sealed class Fixture103
{
    public sealed record Indication(int Fun, int Inf, int Dpi, bool Gi);

    public sealed record Value(int Raw, bool Ov, bool Er);

    public sealed record Measurand(int Fun, int Inf, int Type, List<Value> Values);

    public string Name { get; private init; } = "";
    public int CommonAddress { get; private init; }
    public int IdentFun { get; private init; }
    public int Col { get; private init; }
    public string Ascii { get; private init; } = "";
    public byte[] Software { get; private init; } = [];
    public List<Indication> Indications { get; } = [];
    public List<Measurand> Measurands { get; } = [];
    public HashSet<(int Fun, int Inf)> Commands { get; } = [];

    /// <summary>A generic entry (contract 2.3): GIN, description, GDD type and size, value, writable.</summary>
    /// <param name="Value">A string (type 1), a long (types 3, 4) or a double (type 7).</param>
    public sealed record GenericEntry(int Group, int Entry, string Description, int Type, int Size, object Value, bool Writable);

    public sealed record Tag(int Pos, int Fun, int Inf, int Dpi);

    public sealed record Channel(int Acc, double Rpv, double Rsv, double Rfa, List<short> Values);

    /// <param name="Time">The time of the fault (ASDU 23), UTC.</param>
    /// <param name="Start">The time of the first value (ASDU 26): milliseconds of the day.</param>
    public sealed record Disturbance(int Fan, bool Tp, bool Tm, bool Test, bool Otev, DateTime Time, int Nof, int Interval,
        int Start, List<Tag> Tags, List<Channel> Channels);

    public List<GenericEntry> Generic { get; } = [];
    public List<Disturbance> Disturbances { get; } = [];

    public static Fixture103 Load(string path)
    {
        JsonElement root;
        try
        {
            using var doc = JsonDocument.Parse(File.ReadAllText(path));
            root = doc.RootElement.Clone();
        }
        catch (Exception e) when (e is IOException or JsonException or UnauthorizedAccessException)
        {
            throw new FixtureException($"cannot read fixture {path}: {e.Message}");
        }

        try
        {
            if (root.GetProperty("schemaVersion").GetString() != "1.0")
            {
                throw new FixtureException("schemaVersion must be 1.0");
            }

            var station = root.GetProperty("station");
            var ident = station.GetProperty("identification");
            var ascii = ident.GetProperty("ascii").GetString() ?? "";
            var software = ident.GetProperty("software").GetString() ?? "";
            if (ascii.Length != 8 || ascii.Any(c => c < 0x20 || c > 0x7e))
            {
                throw new FixtureException("identification.ascii must be eight printable ASCII characters");
            }

            if (software.Length != 8)
            {
                throw new FixtureException("identification.software must be four octets in hex");
            }

            var fx = new Fixture103
            {
                Name = root.GetProperty("name").GetString() ?? "",
                CommonAddress = Octet(station.GetProperty("commonAddress"), 254, "station.commonAddress"),
                IdentFun = Octet(ident.GetProperty("fun"), 255, "identification.fun"),
                Col = Octet(ident.GetProperty("col"), 255, "identification.col"),
                Ascii = ascii,
                Software = Convert.FromHexString(software),
            };
            foreach (var i in root.GetProperty("indications").EnumerateArray())
            {
                var gi = !i.TryGetProperty("gi", out var g) || g.GetBoolean();
                var ind = new Indication(Octet(i.GetProperty("fun"), 254, "fun"), Octet(i.GetProperty("inf"), 255, "inf"),
                    Octet(i.GetProperty("dpi"), 3, "dpi"), gi);
                if (fx.Indications.Any(x => x.Fun == ind.Fun && x.Inf == ind.Inf))
                {
                    throw new FixtureException($"indication {ind.Fun}/{ind.Inf}: duplicate");
                }

                fx.Indications.Add(ind);
            }

            if (root.TryGetProperty("measurands", out var ms))
            {
                foreach (var m in ms.EnumerateArray())
                {
                    var type = m.GetProperty("type").GetInt32();
                    var values = m.GetProperty("values").EnumerateArray().Select(v => new Value(
                        v.GetProperty("value").GetInt32(),
                        v.TryGetProperty("ov", out var ov) && ov.GetBoolean(),
                        v.TryGetProperty("er", out var er) && er.GetBoolean())).ToList();
                    if (type is not (3 or 9) || values.Count < 1 || values.Count > (type == 3 ? 4 : 9) ||
                        values.Any(v => v.Raw is < -4096 or > 4095))
                    {
                        throw new FixtureException("a measurand has a type other than 3 or 9, too many values or a value out of -4096..4095");
                    }

                    fx.Measurands.Add(new Measurand(Octet(m.GetProperty("fun"), 254, "fun"), Octet(m.GetProperty("inf"), 255, "inf"), type, values));
                }
            }

            if (root.TryGetProperty("commands", out var cs))
            {
                foreach (var c in cs.EnumerateArray())
                {
                    var key = (Octet(c.GetProperty("fun"), 254, "fun"), Octet(c.GetProperty("inf"), 255, "inf"));
                    if (!fx.Indications.Any(x => x.Fun == key.Item1 && x.Inf == key.Item2))
                    {
                        throw new FixtureException($"command {key.Item1}/{key.Item2}: no indication with this function type and information number");
                    }

                    if (!fx.Commands.Add(key))
                    {
                        throw new FixtureException($"command {key.Item1}/{key.Item2}: duplicate");
                    }
                }
            }

            if (root.TryGetProperty("generic", out var gs))
            {
                foreach (var g in gs.EnumerateArray())
                {
                    var gin = g.GetProperty("gin");
                    var type = g.GetProperty("type").GetInt32();
                    var v = g.GetProperty("value");
                    object value = type switch
                    {
                        1 => v.GetString() ?? "",
                        3 or 4 => v.GetInt64(),
                        7 => v.GetDouble(),
                        _ => throw new FixtureException($"generic entry: data type {type} is not 1, 3, 4 or 7"),
                    };
                    fx.Generic.Add(new GenericEntry(Octet(gin.GetProperty("group"), 255, "gin.group"), Octet(gin.GetProperty("entry"), 255, "gin.entry"),
                        g.GetProperty("description").GetString() ?? "", type, Octet(g.GetProperty("size"), 32, "size"), value,
                        g.TryGetProperty("writable", out var w) && w.GetBoolean()));
                }
            }

            if (root.TryGetProperty("disturbances", out var ds))
            {
                foreach (var d in ds.EnumerateArray())
                {
                    bool Sof(string bit) => d.TryGetProperty("sof", out var s) && s.TryGetProperty(bit, out var b) && b.GetBoolean();
                    var start = d.GetProperty("start").GetString() ?? "";
                    if (!TimeSpan.TryParseExact(start, @"hh\:mm\:ss\.fff", CultureInfo.InvariantCulture, out var startSpan))
                    {
                        throw new FixtureException($"disturbance start '{start}' is not HH:MM:SS.mmm");
                    }

                    fx.Disturbances.Add(new Disturbance(d.GetProperty("fan").GetInt32(), Sof("tp"), Sof("tm"), Sof("test"), Sof("otev"),
                        AsduDoc.ParseTime(d.GetProperty("time").GetString()), d.GetProperty("nof").GetInt32(), d.GetProperty("interval").GetInt32(),
                        (int)startSpan.TotalMilliseconds,
                        d.GetProperty("tags").EnumerateArray().Select(t => new Tag(t.GetProperty("pos").GetInt32(), Octet(t.GetProperty("fun"), 254, "fun"),
                            Octet(t.GetProperty("inf"), 255, "inf"), Octet(t.GetProperty("dpi"), 3, "dpi"))).ToList(),
                        d.GetProperty("channels").EnumerateArray().Select(c => new Channel(Octet(c.GetProperty("acc"), 255, "acc"),
                            c.GetProperty("rpv").GetDouble(), c.GetProperty("rsv").GetDouble(), c.GetProperty("rfa").GetDouble(),
                            c.GetProperty("values").EnumerateArray().Select(x => (short)x.GetInt32()).ToList())).ToList()));
                }
            }

            return fx;
        }
        catch (Exception e) when (e is KeyNotFoundException or InvalidOperationException or FormatException)
        {
            throw new FixtureException($"invalid fixture {path}: {e.Message}");
        }
    }

    private static int Octet(JsonElement e, int max, string what)
    {
        var n = e.GetInt32();
        if (n < 0 || n > max)
        {
            throw new FixtureException($"{what}: {n} is not 0..{max}");
        }

        return n;
    }
}
