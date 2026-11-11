// SPDX-License-Identifier: GPL-3.0-or-later
//
// The generic services and the transmission of disturbance data of IEC 103
// (contract 2.3, docs/CONTAINER_CONTRACT.md "IEC 103 client"): the
// interfaces an adapter implements with its library's own procedures, and
// the documents of their results. Nothing here builds or sends an ASDU; the
// documents only present what the library decoded.

using System.Globalization;

namespace Interop103;

/// <summary>The station answered with a negative cause (41 or 43).</summary>
public sealed class NegativeAnswerException(int cot, List<Dictionary<string, object?>> sets, string message) : Exception(message)
{
    public int Cot { get; } = cot;

    public List<Dictionary<string, object?>> Sets { get; } = sets;
}

/// <summary>The generic services of a library (clause 7.4.8). Methods throw
/// TimeoutException when no answer came, NegativeAnswerException for cause 41
/// or 43, NotSentException when the library would not send the request.</summary>
public interface IGeneric103
{
    /// <summary>Read attribute kod of the single entry group.entry; returns the cause and the data sets of the answer.</summary>
    (int Cot, List<Dictionary<string, object?>> Sets) GenericRead(int ca, int group, int entry, int kod, int timeoutMs, Action<byte[]> received);

    /// <summary>Write the value (a data set of the given type and size) to attribute kod of group.entry, with execution.</summary>
    (int Cot, List<Dictionary<string, object?>> Sets) GenericWrite(int ca, int group, int entry, int kod, int type, int size, string value,
        int timeoutMs, Action<byte[]> received);
}

/// <summary>The transmission of disturbance data of a library (clause 7.4.7).</summary>
public interface IDisturbance103
{
    /// <summary>fun -1 is the library's choice (an image refuses a --fun it cannot send, as a usage error).
    /// The faults of the list of recorded disturbances (ASDU 23) the station answers with: Disturbances.Fault documents.</summary>
    List<Dictionary<string, object?>> DisturbanceList(int ca, int fun, int timeoutMs, Action<byte[]> received);

    /// <summary>The whole transfer of fault fan; returns its Disturbances.Record document.</summary>
    Dictionary<string, object?> DisturbanceTransfer(int ca, int fun, int fan, int timeoutMs, Action<byte[]> received);
}

/// <summary>Documents of generic data sets.</summary>
public static class GenericDoc
{
    /// <summary>"G.E" into its group and entry.</summary>
    public static (int Group, int Entry) ParseGin(string? s)
    {
        var parts = s?.Split('.') ?? [];
        if (parts.Length != 2 || !int.TryParse(parts[0], out var g) || !int.TryParse(parts[1], out var e) || g is < 0 or > 255 || e is < 0 or > 255)
        {
            throw new UsageException($"--gin: '{s}' is not GROUP.ENTRY, each 0..255");
        }

        return (g, e);
    }

    /// <summary>One data set: GIN, KOD, the GDD and the values of the GID as the data type lays them out.</summary>
    public static Dictionary<string, object?> Set(int group, int entry, int kod, int type, int size, int number, ReadOnlySpan<byte> gid)
    {
        var doc = new Dictionary<string, object?>
        {
            ["gin"] = $"{group}.{entry}", ["kod"] = kod, ["type"] = type, ["size"] = size, ["number"] = number,
        };
        var values = new List<object?>();
        switch (type)
        {
            case 1:
                values.Add(System.Text.Encoding.ASCII.GetString(gid).TrimEnd('\0'));
                break;
            case 3 or 4 or 22 when size is >= 1 and <= 8 && gid.Length == size * number:
                for (var i = 0; i < number; i++)
                {
                    long v = 0;
                    for (var k = 0; k < size; k++)
                    {
                        v |= (long)gid[i * size + k] << (8 * k);
                    }

                    if (type == 4 && size < 8 && (gid[i * size + size - 1] & 0x80) != 0)
                    {
                        v |= -1L << (8 * size);
                    }

                    values.Add(v);
                }

                break;
            case 7 when size == 4 && gid.Length == 4 * number:
                for (var i = 0; i < number; i++)
                {
                    values.Add((double)BitConverter.ToSingle(gid[(4 * i)..(4 * i + 4)]));
                }

                break;
            case 8 when size == 8 && gid.Length == 8 * number:
                for (var i = 0; i < number; i++)
                {
                    values.Add(BitConverter.ToDouble(gid[(8 * i)..(8 * i + 8)]));
                }

                break;
            default:
                doc["data"] = AsduDoc.Hex(gid);
                return doc;
        }

        doc["values"] = values;
        return doc;
    }

    /// <summary>The value of --value for a data type: a long (3, 4), a double (7) or the text (1).</summary>
    public static object ParseValue(int type, int size, string? s)
    {
        if (s is null)
        {
            throw new UsageException("generic-write: --value is required");
        }

        switch (type)
        {
            case 1:
                if (s.Length != size || s.Any(c => c < 0x20 || c > 0x7e))
                {
                    throw new UsageException($"--value: '{s}' is not {size} printable ASCII characters");
                }

                return s;
            case 3 or 4:
                if (size is not (1 or 2 or 4) || !long.TryParse(s, NumberStyles.AllowLeadingSign, CultureInfo.InvariantCulture, out var n) ||
                    (type == 3 ? n < 0 || n >= 1L << (8 * size) : n < -(1L << (8 * size - 1)) || n >= 1L << (8 * size - 1)))
                {
                    throw new UsageException($"--value: '{s}' is not an integer of {size} octets (--size 1, 2 or 4)");
                }

                return n;
            case 7:
                if (size != 4 || !double.TryParse(s, NumberStyles.Float, CultureInfo.InvariantCulture, out var d))
                {
                    throw new UsageException($"--value: '{s}' is not a number (--size 4)");
                }

                return d;
            default:
                throw new UsageException($"--type: {type} is not 1, 3, 4 or 7");
        }
    }
}

/// <summary>Documents of the transmission of disturbance data.</summary>
public static class Disturbances
{
    /// <summary>One entry of ASDU 23: FAN, SOF (its four bits, clause 7.2.6.24) and the time of the fault.</summary>
    public static Dictionary<string, object?> Fault(int fan, bool tp, bool tm, bool test, bool otev, string time) => new()
    {
        ["fan"] = fan,
        ["sof"] = new Dictionary<string, object?> { ["tp"] = tp, ["tm"] = tm, ["test"] = test, ["otev"] = otev },
        ["time"] = time,
    };

    /// <summary>A transferred record. start is the time of the first value (ASDU 26) as HH:MM:SS.mmm. A field the
    /// library does not report is null (a tag position: -1).</summary>
    public static Dictionary<string, object?> Record(int fan, int? tov, int? nof, int noc, int noe, int interval, string start,
        IEnumerable<(int Pos, int Fun, int Inf, int Dpi)> tags,
        IEnumerable<(int Acc, double Rpv, double Rsv, double Rfa, IEnumerable<int> Values)> channels) => new()
    {
        ["fan"] = fan, ["tov"] = tov, ["nof"] = nof, ["noc"] = noc, ["noe"] = noe, ["interval"] = interval, ["start"] = start,
        ["tags"] = tags.Select(t => new Dictionary<string, object?> { ["pos"] = t.Pos < 0 ? null : t.Pos, ["fun"] = t.Fun, ["inf"] = t.Inf, ["dpi"] = t.Dpi }).ToList(),
        ["channels"] = channels.Select(c => new Dictionary<string, object?>
        {
            ["acc"] = c.Acc, ["rpv"] = c.Rpv, ["rsv"] = c.Rsv, ["rfa"] = c.Rfa, ["values"] = c.Values.ToList(),
        }).ToList(),
    };

    /// <summary>A CP32Time2a, or the first four octets of a CP56Time2a, as HH:MM:SS.mmm.</summary>
    public static string Time32(int ms, int minute, int hour) =>
        $"{hour & 0x1f:D2}:{minute & 0x3f:D2}:{ms / 1000:D2}.{ms % 1000:D3}";
}
