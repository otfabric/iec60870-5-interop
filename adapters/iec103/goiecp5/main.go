// SPDX-License-Identifier: GPL-3.0-or-later

// Command iec60870-5-interop is the IEC 60870-5-103 adapter of
// iec60870-5-interop for riclolsen/go-iecp5 (package cs103): an IEC 103
// control system (primary station, master) whose FT 1.2 byte stream runs over
// one TCP connection, as through a terminal server. cs103 has no secondary
// (protection equipment) side, so the image is a client only.
//
// It implements the container contract in docs/CONTAINER_CONTRACT.md.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	adapter       = "goiecp5"
	schemaVersion = "1.0"
	standard      = "iec60870-5-103"
	defaultPort   = 2401

	exitOK              = 0
	exitOperationFailed = 1
	exitUsage           = 2
	exitConnectFailed   = 3
)

// Set by the linker; see the Dockerfile.
var (
	adapterVersion  = "dev"
	upstreamVersion = "unknown" // riclolsen/go-iecp5
)

const usage = `usage: iec60870-5-interop <command>

  client <operation> --host HOST [--port N] [--common-address N]
         [--timeout-ms N] [--connect-timeout-ms N] [--collect-ms N] [LINK] [operation flags]
      connect      [--hold-ms N]
      interrogate  [--scn N]
      clock-sync   [--time 2026-01-02T03:04:05.678Z]
      command      --fun N --inf N --dco 1|2|off|on [--rii N]
      monitor      [--duration-ms N] [--max-asdus N]
  print-capabilities
  print-fixture [NAME]
  version

  LINK: [--link-mode unbalanced] [--link-address N]

This image has no server: riclolsen/go-iecp5 implements the IEC 103 primary
station only. See docs/CONTAINER_CONTRACT.md in
https://github.com/otfabric/iec60870-5-interop
`

type object = map[string]any

var outMu sync.Mutex

// emit prints one JSON document on a single line of stdout.
func emit(doc any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		logf("cannot encode document: %v", err)
		return
	}
	outMu.Lock()
	_, _ = os.Stdout.Write(buf.Bytes())
	outMu.Unlock()
}

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "["+adapter+"] "+format+"\n", a...)
}

func printCapabilities() int {
	emit(object{
		"schemaVersion":  schemaVersion,
		"adapter":        adapter,
		"adapterVersion": adapterVersion,
		"protocol":       standard,
		"standard":       standard,
		"transport":      "tcp-stream",
		"linkModes":      []string{"unbalanced"},
		"upstream": object{
			"name":     "riclolsen/go-iecp5",
			"version":  upstreamVersion,
			"revision": "github.com/riclolsen/go-iecp5@" + upstreamVersion,
			// The LICENSE file is LGPL-3.0; the README and the cs103 file
			// headers say GPL-3.0 (docs/REFERENCE_STACKS.md).
			"license":           "LGPL-3.0 (LICENSE file); GPL-3.0 in README and file headers",
			"language":          "Go",
			"independentEngine": true,
		},
		"roles": object{"server": false, "client": true},
		"features": object{
			"generalInterrogation":      true,
			"groupInterrogation":        false,
			"counterInterrogation":      false,
			"read":                      false,
			"clockSync":                 true,
			"testCommand":               false,
			"directCommands":            true,
			"selectBeforeOperate":       false,
			"commandDeactivation":       false,
			"timeTaggedCommands":        false,
			"spontaneousOnCommand":      false,
			"apciParameters":            false,
			"multipleConnections":       false,
			"pointQualityOnBitstring":   false,
			"tls":                       false,
			"tlsServer":                 false,
			"tlsClient":                 false,
			"dataTransferEvents":        false,
			"fileTransfer":              false,
			"fileServer":                false,
			"fileClient":                false,
			"fileUploadServer":          false,
			"fileUploadClient":          false,
			"fileDirectoryServer":       false,
			"fileDirectoryClient":       false,
			"redundancyServer":          false,
			"redundancyClient":          false,
			"secureAuthentication":      false,
			"unbalancedMode":            true,
			"balancedMode":              false,
			"distinctBalancedAddresses": false,
			"generalCommand":            true,
			"identification":            true,
			"measurands":                true,
			"broadcastClockSync":        false,
			// cs103 defines the type identifications 10, 11, 21 and 23..31
			// but builds and decodes none of them (asdu.go: "Disturbance
			// data transfer (not implemented by this package)").
			"genericServices":       false,
			"disturbanceList":       false,
			"disturbanceData":       false,
			"disturbanceDataServer": false,
		},
		"pointTypes":       []string{},
		"commandTypes":     []string{},
		"asduTypes":        object{"monitor": []int{1, 2, 3, 4, 5, 6, 8, 9}, "control": []int{6, 7, 20}},
		"clientOperations": []string{"connect", "interrogate", "clock-sync", "command", "monitor"},
	})
	return exitOK
}

func printFixture(name string) int {
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		logf("print-fixture: '%s' is not a fixture name", name)
		return exitUsage
	}
	raw, err := os.ReadFile(filepath.Join("/fixtures", name, "fixture.json"))
	if err != nil {
		logf("print-fixture: no fixture named '%s' in this image", name)
		return exitOperationFailed
	}
	_, _ = os.Stdout.Write(raw)
	return exitOK
}

func run(argv []string) (int, error) {
	if len(argv) < 1 {
		fmt.Fprint(os.Stderr, usage)
		return exitUsage, nil
	}
	switch argv[0] {
	case "server":
		return exitUsage, usagef("server: this image has no server (riclolsen/go-iecp5 cs103 is a primary station only)")
	case "client":
		if len(argv) < 2 {
			fmt.Fprint(os.Stderr, usage)
			return exitUsage, nil
		}
		return runClient(argv[1], &args{rest: argv[2:]})
	case "print-capabilities":
		return printCapabilities(), nil
	case "print-fixture":
		name := "baseline"
		if len(argv) > 1 {
			name = argv[1]
		}
		return printFixture(name), nil
	case "version":
		fmt.Printf("%s %s (riclolsen/go-iecp5 %s, IEC 60870-5-103)\n", adapter, adapterVersion, upstreamVersion)
		return exitOK, nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return exitOK, nil
	}
	fmt.Fprint(os.Stderr, usage)
	return exitUsage, nil
}

func main() {
	code, err := run(os.Args[1:])
	var ue usageError
	switch {
	case errors.As(err, &ue):
		logf("%s", ue.msg)
		code = exitUsage
	case err != nil:
		logf("fatal: %v", err)
		code = exitOperationFailed
	}
	os.Exit(code)
}
