#!/usr/bin/env bash
# Fails when versions.yaml disagrees with what the builds actually pin.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
status=0

need() {  # need <description> <value that must appear in versions.yaml>
    if grep -qF -- "$2" versions.yaml; then
        echo "ok   $1: $2"
    else
        echo "FAIL $1: $2 is not in versions.yaml"
        status=1
    fi
}

arg() { sed -nE "s/^ARG $2=(.*)$/\1/p" "$1" | head -1; }

need "lib60870 version"  "$(arg adapters/iec104/lib60870/Dockerfile LIB60870_VERSION)"
need "lib60870 revision" "$(arg adapters/iec104/lib60870/Dockerfile LIB60870_SHA)"
need "debian digest"     "$(arg adapters/iec104/lib60870/Dockerfile DEBIAN_DIGEST)"
need "mbedtls version"   "mbedtls: \"$(arg adapters/iec104/lib60870/Dockerfile MBEDTLS_VERSION)\""
need "mbedtls sha256"    "$(arg adapters/iec104/lib60870/Dockerfile MBEDTLS_SHA256)"
need "maven digest"      "$(arg adapters/iec104/openmuc/Dockerfile MAVEN_DIGEST)"
need "jre digest"        "$(arg adapters/iec104/openmuc/Dockerfile JRE_DIGEST)"
need "j60870 version"    "version: \"$(arg adapters/iec104/openmuc/Dockerfile J60870_VERSION)\""
if [[ "$(arg adapters/iec104/openmuc/Dockerfile J60870_VERSION)" != "$(sed -nE 's|.*<j60870.version>(.*)</j60870.version>.*|\1|p' adapters/iec104/openmuc/pom.xml)" ]]; then
    echo "FAIL j60870 version: Dockerfile and pom.xml disagree"
    status=1
fi
need "gson version"      "gson: \"$(sed -nE 's|.*<gson.version>(.*)</gson.version>.*|\1|p' adapters/iec104/openmuc/pom.xml)\""

gomod() { sed -nE "s|^[[:space:]]*github.com/wendy512/$1 (v[^ ]+).*$|\\1|p" adapters/iec104/wendy512/go.mod | head -1; }
need "golang digest"     "$(arg adapters/iec104/wendy512/Dockerfile GOLANG_DIGEST)"
need "wendy512 debian"   "$(arg adapters/iec104/wendy512/Dockerfile DEBIAN_DIGEST)"
need "iec104 version"    "version: \"$(arg adapters/iec104/wendy512/Dockerfile IEC104_VERSION)\""
need "go-iecp5 version"  "version: \"$(arg adapters/iec104/wendy512/Dockerfile GOIECP5_VERSION)\""
if [[ "$(arg adapters/iec104/wendy512/Dockerfile IEC104_VERSION)" != "$(gomod iec104)" ]]; then
    echo "FAIL iec104 version: Dockerfile and go.mod disagree"
    status=1
fi
if [[ "$(arg adapters/iec104/wendy512/Dockerfile GOIECP5_VERSION)" != "$(gomod go-iecp5)" ]]; then
    echo "FAIL go-iecp5 version: Dockerfile and go.mod disagree"
    status=1
fi
cb=$(arg adapters/iec104/cbsigma/Dockerfile IEC60870_VERSION)
need "cbsigma golang"    "$(arg adapters/iec104/cbsigma/Dockerfile GOLANG_DIGEST)"
need "cbsigma debian"    "$(arg adapters/iec104/cbsigma/Dockerfile DEBIAN_DIGEST)"
need "iec60870 version"  "version: \"${cb}\""
if [[ "${cb}" != "$(sed -nE 's|^require github.com/cB-Sigma/iec60870 (v[^ ]+).*$|\1|p' adapters/iec104/cbsigma/go.mod)" ]]; then
    echo "FAIL iec60870 version: Dockerfile and go.mod disagree"
    status=1
fi
p5=$(arg adapters/iec104/part5/Dockerfile PART5_VERSION)
need "part5 golang"      "$(arg adapters/iec104/part5/Dockerfile GOLANG_DIGEST)"
need "part5 debian"      "$(arg adapters/iec104/part5/Dockerfile DEBIAN_DIGEST)"
# The part5 entry of implementations, so a version another entry has cannot match.
p5yaml=$(awk '/^  iec104-part5:/{on=1; next} on && /^  [a-z0-9-]+:/{on=0} on && /^    version:/{print $2; exit}' versions.yaml | tr -d '"')
if [[ "${p5yaml}" == "${p5}" ]]; then echo "ok   part5 version: ${p5}"; else echo "FAIL part5 version: Dockerfile ${p5}, versions.yaml '${p5yaml}'"; status=1; fi
if [[ "${p5}" != "$(sed -nE 's|^require github.com/pascaldekloe/part5 (v[^ ]+).*$|\1|p' adapters/iec104/part5/go.mod)" ]]; then
    echo "FAIL part5 version: Dockerfile and go.mod disagree"
    status=1
fi

# ---- IEC 101: the same upstream pins as the IEC 104 images ----
for v in LIB60870_VERSION LIB60870_SHA DEBIAN_DIGEST MBEDTLS_VERSION MBEDTLS_SHA256; do
    if [[ "$(arg adapters/iec101/lib60870/Dockerfile "$v")" != "$(arg adapters/iec104/lib60870/Dockerfile "$v")" ]]; then
        echo "FAIL iec101 lib60870 ${v}: differs from the iec104 adapter"
        status=1
    else
        echo "ok   iec101 lib60870 ${v}"
    fi
done
cb101=$(arg adapters/iec101/cbsigma/Dockerfile IEC60870_VERSION)
need "iec101 cbsigma golang" "$(arg adapters/iec101/cbsigma/Dockerfile GOLANG_DIGEST)"
need "iec101 cbsigma debian" "$(arg adapters/iec101/cbsigma/Dockerfile DEBIAN_DIGEST)"
need "iec101 iec60870 version" "version: \"${cb101}\""
if [[ "${cb101}" != "$(sed -nE 's|^require github.com/cB-Sigma/iec60870 (v[^ ]+).*$|\1|p' adapters/iec101/cbsigma/go.mod)" ]]; then
    echo "FAIL iec101 iec60870 version: Dockerfile and go.mod disagree"
    status=1
fi
# ---- IEC 102 ----
gd=adapters/iec102/gisce
need "iec102 gisce python"   "$(arg ${gd}/Dockerfile PYTHON_DIGEST)"
need "iec102 gisce version"  "version: \"$(sed -nE 's/^ARG IEC870REE_VERSION=(.*)$/\1/p' ${gd}/Dockerfile | head -1)\""
need "iec102 gisce revision" "$(sed -nE 's/^ARG IEC870REE_SHA=(.*)$/\1/p' ${gd}/Dockerfile | head -1)"
if [[ "$(grep -c '^ARG IEC870REE_SHA=' ${gd}/Dockerfile)" != 2 || "$(sed -nE 's/^ARG IEC870REE_SHA=(.*)$/\1/p' ${gd}/Dockerfile | sort -u | wc -l | tr -d ' ')" != 1 ]]; then
    echo "FAIL iec102 gisce: the two stages of the Dockerfile pin different revisions"
    status=1
fi
need "iec102 meter python"   "$(sed -nE 's/.*python:3.12-slim-bookworm@(sha256:[0-9a-f]+).*/\1/p' scripts/smoke-iec102.sh | head -1)"
while read -r pkg ver; do
    need "iec102 gisce ${pkg}" "pypi_${pkg//-/_}: \"${ver}\""
done < <(sed -nE 's/^([a-z-]+)==([^ ]+).*/\1 \2/p' ${gd}/requirements.txt ${gd}/build-requirements.txt)
# ---- IEC 103 ----
gi=$(arg adapters/iec103/goiecp5/Dockerfile GOIECP5_VERSION)
need "iec103 goiecp5 golang" "$(arg adapters/iec103/goiecp5/Dockerfile GOLANG_DIGEST)"
need "iec103 goiecp5 debian" "$(arg adapters/iec103/goiecp5/Dockerfile DEBIAN_DIGEST)"
need "go-iecp5 (riclolsen) version" "version: \"${gi}\""
if [[ "${gi}" != "$(sed -nE 's|^require github.com/riclolsen/go-iecp5 (v[^ ]+).*$|\1|p' adapters/iec103/goiecp5/go.mod)" ]]; then
    echo "FAIL go-iecp5 (riclolsen) version: Dockerfile and go.mod disagree"
    status=1
fi
for pair in vitusdong:VITUSDONG ariec103:ARIEC103 open103:OPEN103; do
    a=${pair%%:*} v=${pair#*:}
    need "iec103 ${a} version"  "version: \"$(arg "adapters/iec103/${a}/Dockerfile" "${v}_VERSION")\""
    need "iec103 ${a} revision" "$(arg "adapters/iec103/${a}/Dockerfile" "${v}_SHA")"
    need "iec103 ${a} sdk"      "$(arg "adapters/iec103/${a}/Dockerfile" SDK_DIGEST)"
    need "iec103 ${a} runtime"  "$(arg "adapters/iec103/${a}/Dockerfile" RUNTIME_DIGEST)"
    [[ -f "adapters/iec103/${a}/packages.lock.json" ]] || { echo "FAIL iec103 ${a}: no packages.lock.json"; status=1; }
done
exit "${status}"
