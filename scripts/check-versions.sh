#!/usr/bin/env bash
# Fails when versions.yaml disagrees with what the builds actually pin: the
# Dockerfiles, the adapters' go.mod, go.sum and pom.xml, the published images
# of the Makefile, its tool images and the govulncheck job of ci.yml. A value
# that is not found in the build files fails too.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
status=0

need() {  # need <description> <value that must appear in versions.yaml>
    if [[ -z "$2" ]]; then
        echo "FAIL $1: no value found in the build files"
        status=1
    elif grep -qF -- "$2" versions.yaml; then
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
    a101="$(arg adapters/iec101/lib60870/Dockerfile "$v")" a104="$(arg adapters/iec104/lib60870/Dockerfile "$v")"
    if [[ -z "${a101}" || -z "${a104}" ]]; then
        echo "FAIL iec101 lib60870 ${v}: not found in both lib60870 Dockerfiles"
        status=1
    elif [[ "${a101}" != "${a104}" ]]; then
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
dp=adapters/iec101/digitalpetri
need "iec101 digitalpetri version"  "version: \"$(arg ${dp}/Dockerfile DIGITALPETRI_VERSION)\""
need "iec101 digitalpetri revision" "$(arg ${dp}/Dockerfile DIGITALPETRI_SHA)"
need "iec101 digitalpetri maven"    "$(arg ${dp}/Dockerfile MAVEN_DIGEST)"
need "iec101 digitalpetri jre"      "$(arg ${dp}/Dockerfile JRE_DIGEST)"
need "iec101 digitalpetri gson"     "gson: \"$(sed -nE 's|.*<gson.version>(.*)</gson.version>.*|\1|p' ${dp}/pom.xml)\""
need "iec101 digitalpetri slf4j"    "slf4j_simple: \"$(sed -nE 's|.*<slf4j.version>(.*)</slf4j.version>.*|\1|p' ${dp}/pom.xml)\""
if [[ "$(grep -c '^ARG DIGITALPETRI_SHA=' ${dp}/Dockerfile)" != 2 || "$(sed -nE 's/^ARG DIGITALPETRI_SHA=(.*)$/\1/p' ${dp}/Dockerfile | sort -u | wc -l | tr -d ' ')" != 1 ]]; then
    echo "FAIL iec101 digitalpetri: the two stages of the Dockerfile pin different revisions"
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

# ---- every implementation: published, and the hash of its source ----
# impl <name> <field> [engine]: a field of implementations.<name>, or of its engine.
impl() {
    awk -v n="$1" -v f="$2" -v e="${3:-}" '
        /^[a-z_]+:/ { sect = $1 }
        sect != "implementations:" { next }
        /^  [a-z0-9-]+:$/ { cur = $1; sub(/:$/, "", cur); part = ""; next }
        cur == n && /^    engine:$/ { part = "engine"; next }
        cur == n && /^    [a-z]/ { part = "" }
        cur == n && part == e && $1 == f ":" { v = $2; gsub(/"/, "", v); print v; exit }
    ' versions.yaml
}
names=" $(awk '/^[a-z_]+:/ { sect = $1 } sect == "implementations:" && /^  [a-z0-9-]+:$/ { sub(/:$/, "", $1); printf "%s ", $1 }' versions.yaml)"
images=" $(awk '/^[a-z_]+:/ { sect = $1 } sect == "images:" && /^  [a-z0-9-]+:/ { sub(/:$/, "", $1); printf "%s ", $1 }' versions.yaml)"
# The images a release publishes: PUBLISHED of the Makefile, IMAGES without UNPUBLISHED.
mk() { sed -nE "s/^$1[[:space:]]*\?=[[:space:]]*(.*)$/\1/p" Makefile | head -1; }
published=" "
for s in iec101 iec102 iec103 iec104; do
    for a in $(mk "$(echo "${s}" | tr '[:lower:]' '[:upper:]')_ADAPTERS"); do published="${published}${s}-${a} "; done
done
for u in $(mk UNPUBLISHED); do published="${published/ ${u} / }"; done
hex() {  # hex <description> <value>: a SHA-256 in hex
    if [[ "$2" =~ ^[0-9a-f]{64}$ ]]; then echo "ok   $1: $2"; else echo "FAIL $1: '$2' is not a SHA-256"; status=1; fi
}
same_sum() {  # same_sum <description> <adapter directory> <module> <version> <module_sum of versions.yaml>
    local got
    got="$(awk -v m="$3" -v v="$4" '$1 == m && $2 == v { print $3; exit }' "$2/go.sum" 2>/dev/null)" || got=""
    if [[ -n "${got}" && "${got}" == "$5" ]]; then echo "ok   $1 module sum: $5"
    else echo "FAIL $1 module sum: $2/go.sum has '${got}' for $3 $4, versions.yaml '$5'"; status=1; fi
}
for n in ${names}; do
    p="$(impl "${n}" published)"
    case "${published}" in *" ${n} "*) want=true ;; *) want=false ;; esac
    if [[ "${p}" != true && "${p}" != false ]]; then
        echo "FAIL ${n} published: '${p}', not true or false"; status=1
    elif [[ "${p}" != "${want}" ]]; then
        echo "FAIL ${n} published: ${p}, but the Makefile (PUBLISHED) says ${want}"; status=1
    elif [[ "${p}" == true && "${images}" != *" ${n} "* ]]; then
        echo "FAIL ${n} published: not in images"; status=1
    else
        echo "ok   ${n} published: ${p}"
    fi
    dir="adapters/${n%%-*}/${n#*-}"
    if [[ -n "$(impl "${n}" module)" ]]; then
        same_sum "${n}" "${dir}" "$(impl "${n}" module)" "$(impl "${n}" version)" "$(impl "${n}" module_sum)"
        if [[ -n "$(impl "${n}" module engine)" ]]; then
            same_sum "${n} engine" "${dir}" "$(impl "${n}" module engine)" "$(impl "${n}" version engine)" "$(impl "${n}" module_sum engine)"
        fi
    elif [[ -n "$(impl "${n}" artifact)" ]]; then
        hex "${n} artifact_sha256" "$(impl "${n}" artifact_sha256)"
    else
        hex "${n} source_sha256" "$(impl "${n}" source_sha256)"
    fi
done
for i in ${published}; do
    [[ "${names}" == *" ${i} "* ]] || { echo "FAIL ${i}: published by the Makefile, not in implementations"; status=1; }
done
hex "gson sha256" "$(sed -nE 's/^  gson_sha256: "([^"]*)".*$/\1/p' versions.yaml)"

# ---- tooling: the tool images of the Makefile by digest, govulncheck of ci.yml ----
tools='$(TOOLS_REGISTRY)/'
while read -r var ref; do
    case "${ref}" in
        "${tools}"*@sha256:*) need "Makefile ${var}" "\"${ref#"${tools}"}\"" ;;
        *) echo "FAIL Makefile ${var}: '${ref}' is not pinned by digest"; status=1 ;;
    esac
done < <(sed -nE 's/^([A-Z0-9_]+_IMAGE)[[:space:]]*\?=[[:space:]]*(.*)$/\1 \2/p' Makefile)
ci=.github/workflows/ci.yml
need "ci.yml govulncheck"    "$(sed -nE 's/^ *GOVULNCHECK_VERSION: "?(v[0-9][^" ]*)"?$/govulncheck@\1"/p' "${ci}" | head -1)"
need "ci.yml govulncheck Go" "$(sed -nE 's/^ *GO_VERSION: "?([0-9][^" ]*)"?$/golang:\1-trixie@/p' "${ci}" | head -1)"
exit "${status}"
