#!/usr/bin/env python3
"""Validate every fixtures/<standard>/<name>/fixture.json.

Two layers, both required:

  1. The structural rules of fixtures/schema/iec60870-5-fixture.schema.json
     (IEC 101, 102, 104) or fixtures/schema/iec60870-5-103-fixture.schema.json
     (IEC 103, whose objects are addressed by function type and information
     number). They are checked with the `jsonschema` package when it is installed; CI installs
     it, so a missing package is an error there (--require-schema).
  2. The cross-field rules a JSON Schema cannot express: unique addresses, and
     commands whose target exists and has the matching type (IEC 103: a
     command acts on the indication with its function type and information
     number; a measurand of type 3 has at most four values).

The adapters apply the same rules when they load a fixture; this script makes
a broken fixture fail before any image is built.

Usage: validate-fixtures.py [--require-schema] [fixture.json ...]
"""

import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SCHEMA = ROOT / "fixtures" / "schema" / "iec60870-5-fixture.schema.json"
SCHEMA_103 = ROOT / "fixtures" / "schema" / "iec60870-5-103-fixture.schema.json"

# The largest information object address of a standard's fixtures: the IEC 101
# fixtures use two-octet addresses (the default of --ioa-size).
MAX_IOA = {"iec101": 65535, "iec102": 65535, "iec103": 255}

# Command type -> the point type it acts on.
COMMAND_TARGET = {
    "C_SC_NA_1": "M_SP_NA_1",
    "C_DC_NA_1": "M_DP_NA_1",
    "C_RC_NA_1": "M_ST_NA_1",
    "C_SE_NA_1": "M_ME_NA_1",
    "C_SE_NB_1": "M_ME_NB_1",
    "C_SE_NC_1": "M_ME_NC_1",
}


def semantic_errors_103(fx):
    errors = []
    indications = {}
    for i in fx.get("indications", []):
        key = (i.get("fun"), i.get("inf"))
        if key in indications:
            errors.append(f"indication {key[0]}/{key[1]}: duplicate function type and information number")
        indications[key] = i
    measurands = set()
    for m in fx.get("measurands", []):
        key = (m.get("fun"), m.get("inf"))
        if key in measurands or key in indications:
            errors.append(f"measurand {key[0]}/{key[1]}: function type and information number already used")
        measurands.add(key)
        if m.get("type") == 3 and len(m.get("values", [])) > 4:
            errors.append(f"measurand {key[0]}/{key[1]}: measurands I (type 3) have at most four values")
    commands = set()
    for c in fx.get("commands", []):
        key = (c.get("fun"), c.get("inf"))
        if key in commands:
            errors.append(f"command {key[0]}/{key[1]}: duplicate function type and information number")
        commands.add(key)
        if key not in indications:
            errors.append(f"command {key[0]}/{key[1]}: no indication with this function type and information number")
    return errors


def semantic_errors(fx):
    errors = []
    points = {}
    for p in fx.get("points", []):
        ioa = p.get("ioa")
        if ioa in points:
            errors.append(f"point {ioa}: duplicate information object address")
        points[ioa] = p
    for p in fx.get("points", []):
        if p.get("type") == "M_IT_NA_1" and "groups" in p:
            errors.append(f"point {p.get('ioa')}: an integrated total has no interrogation group")
    seen = set()
    for c in fx.get("commands", []):
        ioa = c.get("ioa")
        if ioa in seen:
            errors.append(f"command {ioa}: duplicate information object address")
        seen.add(ioa)
        if ioa in points:
            errors.append(f"command {ioa}: address is also used by a point")
        target = points.get(c.get("target"))
        want = COMMAND_TARGET.get(c.get("type"))
        if target is None:
            errors.append(f"command {ioa}: target {c.get('target')} is not a point")
        elif target["type"] != want:
            errors.append(f"command {ioa}: {c['type']} needs a {want} target, {c['target']} is {target['type']}")
    files = set()
    for f in fx.get("files", []):
        ioa = f.get("ioa")
        if ioa in files:
            errors.append(f"file {ioa}: duplicate information object address")
        files.add(ioa)
        if ioa in points or ioa in seen:
            errors.append(f"file {ioa}: address is also used by a point or a command")
        if isinstance(f.get("sectionSize"), int) and isinstance(f.get("size"), int) and f["sectionSize"] > f["size"]:
            errors.append(f"file {ioa}: sectionSize is larger than size")
    uploads = set()
    for u in fx.get("uploads", []):
        ioa = u.get("ioa")
        if ioa in uploads:
            errors.append(f"upload {ioa}: duplicate information object address")
        uploads.add(ioa)
        if ioa in points or ioa in seen or ioa in files:
            errors.append(f"upload {ioa}: address is also used by a point, a command or a file")
    return errors


def main(argv):
    require_schema = "--require-schema" in argv
    paths = [Path(a) for a in argv if not a.startswith("--")]
    if not paths:
        paths = sorted((ROOT / "fixtures").glob("*/*/fixture.json"))
    if not paths:
        print("no fixtures found", file=sys.stderr)
        return 1

    validators = None
    try:
        import jsonschema

        validators = {}
        for key, path in (("default", SCHEMA), ("iec103", SCHEMA_103)):
            schema = json.loads(path.read_text())
            jsonschema.Draft202012Validator.check_schema(schema)
            validators[key] = jsonschema.Draft202012Validator(schema)
    except ImportError:
        if require_schema:
            print("the jsonschema package is required (pip install jsonschema)", file=sys.stderr)
            return 1
        print("note: jsonschema not installed, schema layer skipped (semantic rules still checked)", file=sys.stderr)

    failed = False
    for path in paths:
        errors = []
        try:
            fx = json.loads(path.read_text())
        except (OSError, ValueError) as e:
            errors.append(str(e))
            fx = None
        standard = path.parent.parent.name
        if fx is not None:
            validator = None
            if validators is not None:
                validator = validators["iec103" if standard == "iec103" else "default"]
            if validator is not None:
                for e in sorted(validator.iter_errors(fx), key=lambda e: list(e.absolute_path)):
                    where = "/".join(str(x) for x in e.absolute_path) or "(root)"
                    errors.append(f"{where}: {e.message}")
            if isinstance(fx, dict) and standard == "iec103":
                errors.extend(semantic_errors_103(fx))
                if path.parent.name != fx.get("name"):
                    errors.append(f"name {fx.get('name')!r} does not match directory {path.parent.name!r}")
            elif isinstance(fx, dict):
                errors.extend(semantic_errors(fx))
                limit = MAX_IOA.get(path.parent.parent.name)
                if limit is not None:
                    for item in fx.get("points", []) + fx.get("commands", []) + fx.get("files", []):
                        if isinstance(item.get("ioa"), int) and item["ioa"] > limit:
                            errors.append(f"{item['ioa']}: larger than the {path.parent.parent.name} limit {limit}")
                if path.parent.name != fx.get("name"):
                    errors.append(f"name {fx.get('name')!r} does not match directory {path.parent.name!r}")
        rel = path.relative_to(ROOT) if path.is_relative_to(ROOT) else path
        if errors:
            failed = True
            print(f"FAIL {rel}")
            for e in errors:
                print(f"     {e}")
        else:
            print(f"ok   {rel}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
