// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** The simulated station: see docs/FIXTURES.md. The rules mirror scripts/validate-fixtures.py. */
final class Fixture {
    static final class InvalidException extends Exception {
        private static final long serialVersionUID = 1L;

        InvalidException(String message) {
            super(message);
        }
    }

    static final class Point {
        int ioa;
        String type;
        double value; // raw NVA for M_ME_NA_1; 0 or 1 for M_SP_NA_1
        boolean transientState;
        int sequence;
        List<String> quality = new ArrayList<>();
        List<Integer> groups = new ArrayList<>();
    }

    static final class Command {
        int ioa;
        String type;
        Point target;
        int reportCause = 11;
        boolean selectRequired;
        boolean selected;
    }

    String name;
    int commonAddress;
    final List<Point> points = new ArrayList<>();
    final List<Command> commands = new ArrayList<>();

    static final Map<String, String> COMMAND_TARGET = Map.of(
            "C_SC_NA_1", "M_SP_NA_1", "C_DC_NA_1", "M_DP_NA_1", "C_RC_NA_1", "M_ST_NA_1",
            "C_SE_NA_1", "M_ME_NA_1", "C_SE_NB_1", "M_ME_NB_1", "C_SE_NC_1", "M_ME_NC_1");

    private static final Map<String, String> ALLOWED_QUALITY = Map.of(
            "M_SP_NA_1", " BL SB NT IV ", "M_DP_NA_1", " BL SB NT IV ", "M_IT_NA_1", " CY CA IV ");

    Point point(int ioa) {
        for (Point p : points) {
            if (p.ioa == ioa) {
                return p;
            }
        }
        return null;
    }

    Command command(int ioa) {
        for (Command c : commands) {
            if (c.ioa == ioa) {
                return c;
            }
        }
        return null;
    }

    private static boolean isInteger(JsonElement e, double lo, double hi) {
        if (e == null || !e.isJsonPrimitive() || !e.getAsJsonPrimitive().isNumber()) {
            return false;
        }
        double d = e.getAsDouble();
        return d == Math.rint(d) && d >= lo && d <= hi;
    }

    private static int intField(JsonObject o, String name, int def) {
        JsonElement e = o.get(name);
        return isInteger(e, Integer.MIN_VALUE, Integer.MAX_VALUE) ? e.getAsInt() : def;
    }

    static Fixture load(Path path) throws InvalidException {
        JsonObject root;
        try {
            root = JsonParser.parseString(Files.readString(path)).getAsJsonObject();
        } catch (java.io.IOException e) {
            throw new InvalidException("cannot read fixture " + path);
        } catch (RuntimeException e) {
            throw new InvalidException("fixture is not valid JSON: " + e.getMessage());
        }
        Fixture fx = new Fixture();
        JsonElement sv = root.get("schemaVersion");
        if (sv == null || !"1.0".equals(sv.getAsString())) {
            throw new InvalidException("unsupported schemaVersion, want 1.0");
        }
        JsonElement name = root.get("name");
        JsonObject station = root.has("station") && root.get("station").isJsonObject()
                ? root.getAsJsonObject("station") : new JsonObject();
        JsonElement points = root.get("points");
        if (name == null || !name.isJsonPrimitive() || name.getAsString().isEmpty()
                || !isInteger(station.get("commonAddress"), 1, 65534)
                || points == null || !points.isJsonArray() || points.getAsJsonArray().isEmpty()) {
            throw new InvalidException("fixture needs name, station.commonAddress (1..65534) and points");
        }
        fx.name = name.getAsString();
        fx.commonAddress = station.get("commonAddress").getAsInt();
        Set<Integer> seen = new HashSet<>();
        for (JsonElement pe : points.getAsJsonArray()) {
            JsonObject o = pe.getAsJsonObject();
            Point p = new Point();
            p.ioa = intField(o, "ioa", 0);
            p.type = o.has("type") ? o.get("type").getAsString() : "";
            if (!Set.of("M_SP_NA_1", "M_DP_NA_1", "M_ST_NA_1", "M_BO_NA_1", "M_ME_NA_1", "M_ME_NB_1",
                    "M_ME_NC_1", "M_IT_NA_1").contains(p.type) || p.ioa < 1 || p.ioa > 0xFFFFFF) {
                throw new InvalidException("point " + p.ioa + ": needs ioa (1..16777215) and a supported type");
            }
            if (!seen.add(p.ioa)) {
                throw new InvalidException("point " + p.ioa + ": duplicate information object address");
            }
            JsonElement v = o.get("value");
            boolean valid;
            switch (p.type) {
                case "M_SP_NA_1" -> {
                    valid = v != null && v.isJsonPrimitive() && v.getAsJsonPrimitive().isBoolean();
                    p.value = valid && v.getAsBoolean() ? 1 : 0;
                }
                case "M_DP_NA_1" -> valid = isInteger(v, 0, 3);
                case "M_ST_NA_1" -> valid = isInteger(v, -64, 63);
                case "M_BO_NA_1" -> valid = isInteger(v, 0, 4294967295.0);
                case "M_ME_NA_1", "M_ME_NB_1" -> valid = isInteger(v, -32768, 32767);
                case "M_ME_NC_1" -> valid = v != null && v.isJsonPrimitive() && v.getAsJsonPrimitive().isNumber();
                default -> valid = isInteger(v, -2147483648.0, 2147483647.0);
            }
            if (!valid) {
                throw new InvalidException("point " + p.ioa + ": value is not valid for " + p.type);
            }
            if (!p.type.equals("M_SP_NA_1")) {
                p.value = v.getAsDouble();
            }
            if (o.has("transient")) {
                if (!p.type.equals("M_ST_NA_1") || !o.get("transient").getAsJsonPrimitive().isBoolean()) {
                    throw new InvalidException("point " + p.ioa + ": transient is a boolean of M_ST_NA_1 only");
                }
                p.transientState = o.get("transient").getAsBoolean();
            }
            if (o.has("sequence")) {
                if (!p.type.equals("M_IT_NA_1") || !isInteger(o.get("sequence"), 0, 31)) {
                    throw new InvalidException("point " + p.ioa + ": sequence is 0..31 of M_IT_NA_1 only");
                }
                p.sequence = o.get("sequence").getAsInt();
            }
            String allowed = ALLOWED_QUALITY.getOrDefault(p.type, " OV BL SB NT IV ");
            if (o.has("quality")) {
                for (JsonElement q : o.getAsJsonArray("quality")) {
                    String f = q.getAsString();
                    if (f.length() != 2 || !allowed.contains(" " + f + " ")) {
                        throw new InvalidException("point " + p.ioa + ": quality is not valid for " + p.type);
                    }
                    p.quality.add(f);
                }
            }
            if (o.has("groups")) {
                JsonArray groups = o.getAsJsonArray("groups");
                if (!groups.isEmpty() && p.type.equals("M_IT_NA_1")) {
                    throw new InvalidException("point " + p.ioa + ": an integrated total has no interrogation group");
                }
                for (JsonElement g : groups) {
                    if (!isInteger(g, 1, 16) || p.groups.contains(g.getAsInt())) {
                        throw new InvalidException("point " + p.ioa + ": groups are unique and 1..16");
                    }
                    p.groups.add(g.getAsInt());
                }
            }
            fx.points.add(p);
        }
        fx.points.sort(Comparator.comparingInt(p -> p.ioa));

        if (root.has("commands")) {
            for (JsonElement ce : root.getAsJsonArray("commands")) {
                JsonObject o = ce.getAsJsonObject();
                Command c = new Command();
                c.ioa = intField(o, "ioa", 0);
                c.type = o.has("type") ? o.get("type").getAsString() : "";
                if (!COMMAND_TARGET.containsKey(c.type) || c.ioa < 1 || c.ioa > 0xFFFFFF) {
                    throw new InvalidException("command " + c.ioa + ": needs ioa, a supported type and target");
                }
                if (!seen.add(c.ioa)) {
                    throw new InvalidException("command " + c.ioa + ": duplicate information object address");
                }
                c.target = fx.point(intField(o, "target", 0));
                if (c.target == null || !c.target.type.equals(COMMAND_TARGET.get(c.type))) {
                    throw new InvalidException("command " + c.ioa + ": target must be a point of the matching type");
                }
                if (o.has("reportCause")) {
                    c.reportCause = intField(o, "reportCause", 0);
                    if (c.reportCause != 3 && c.reportCause != 11) {
                        throw new InvalidException("command " + c.ioa + ": reportCause must be 3 or 11");
                    }
                }
                if (o.has("selectRequired")) {
                    c.selectRequired = o.get("selectRequired").getAsBoolean();
                }
                fx.commands.add(c);
            }
        }
        for (String part : new String[] {"files", "uploads"}) {
            if (root.has(part)) {
                for (JsonElement fe : root.getAsJsonArray(part)) {
                    JsonObject o = fe.getAsJsonObject();
                    int ioa = intField(o, "ioa", 0);
                    int nof = intField(o, "name", 0);
                    if (ioa < 1 || ioa > 0xFFFFFF || nof < 1 || nof > 255) {
                        throw new InvalidException(part + ": needs ioa and name (1..255)");
                    }
                    if (part.equals("files")) {
                        int size = intField(o, "size", 0);
                        int section = intField(o, "sectionSize", 0);
                        if (size < 1 || size > 65536 || section < 1 || section > size) {
                            throw new InvalidException("file " + ioa + ": needs size (1..65536) and sectionSize (1..size)");
                        }
                    } else {
                        int max = intField(o, "maxSize", 0);
                        if (max < 1 || max > 65536) {
                            throw new InvalidException("upload " + ioa + ": needs maxSize (1..65536)");
                        }
                    }
                    if (!seen.add(ioa)) {
                        throw new InvalidException(part + " " + ioa + ": duplicate information object address");
                    }
                }
            }
        }
        return fx;
    }

    boolean hasGroup(int group) {
        for (Point p : points) {
            if (p.groups.contains(group)) {
                return true;
            }
        }
        return false;
    }
}
