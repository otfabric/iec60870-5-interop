// SPDX-License-Identifier: MIT

// Command iec60870-5-interop is OT Fabric's own IEC 60870-5-103 adapter of
// iec60870-5-interop: github.com/otfabric/go-iec60870-5 (packages iec103,
// iec103/server and iec103/client) behind the container contract
// (docs/CONTAINER_CONTRACT.md, "IEC 101, 102 and 103" and "IEC 103"). The
// FT 1.2 byte stream runs over one TCP connection, as through a terminal
// server. The image has both roles: the protection equipment (server) and
// the control system (client).
//
// It is not an independent implementation: it tests the library against
// the reference stacks of the harness.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	adapter       = "otfabric"
	schemaVersion = "1.0"
	standard      = "iec60870-5-103"
	defaultPort   = 2401

	exitOK              = 0
	exitOperationFailed = 1
	exitUsage           = 2
	exitConnectFailed   = 3
	exitFixture         = 4
)

// Set by the linker; see own/go-iec60870-5/Dockerfile.
var (
	adapterVersion   = "dev"
	upstreamVersion  = "unknown"
	upstreamRevision = "unknown"
)

const usage = `usage: iec60870-5-interop <command>

  server [--fixture PATH] [--bind-address ADDR] [--port N] [--ready-file PATH]
         [--common-address N] [LINK]
  client <operation> --host HOST [--port N] [--common-address N]
         [--timeout-ms N] [--connect-timeout-ms N] [--collect-ms N] [LINK] [operation flags]
      connect      [--hold-ms N]
      interrogate  [--scn N]
      clock-sync   [--time 2026-01-02T03:04:05.678Z] [--broadcast]
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

See docs/CONTAINER_CONTRACT.md in https://github.com/otfabric/iec60870-5-interop
`

type object = map[string]any

var (
	outMu  sync.Mutex
	stdout io.Writer = os.Stdout
)

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
	_, _ = stdout.Write(buf.Bytes())
	outMu.Unlock()
}

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "["+adapter+"] "+format+"\n", a...)
}

// clientOperations are the operations of client, in the order of the
// contract.
var clientOperations = []string{
	"connect", "interrogate", "clock-sync", "command", "monitor",
	"generic-read", "generic-write", "disturbance-list", "disturbance-transfer",
}

func capabilities() object {
	return object{
		"schemaVersion":  schemaVersion,
		"adapter":        adapter,
		"adapterVersion": adapterVersion,
		"protocol":       standard,
		"standard":       standard,
		"transport":      "tcp-stream",
		"linkModes":      []string{"unbalanced"},
		"upstream": object{
			"name":              "otfabric/go-iec60870-5",
			"version":           upstreamVersion,
			"revision":          upstreamRevision,
			"license":           "MIT",
			"language":          "Go",
			"independentEngine": false,
		},
		"roles": object{"server": true, "client": true},
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
			// Client.Broadcast (SEND/NO REPLY to link address 255) and a
			// server that takes a broadcast ASDU 6 (Config.OnTimeSync).
			"broadcastClockSync":    true,
			"genericServices":       true,
			"disturbanceList":       true,
			"disturbanceData":       true,
			"disturbanceDataServer": true,
		},
		"pointTypes":   []string{},
		"commandTypes": []string{},
		// monitor: what the client decodes; control: what it sends.
		"asduTypes": object{
			"monitor": []int{1, 2, 3, 4, 5, 6, 8, 9, 10, 11, 23, 26, 27, 28, 29, 30, 31},
			"control": []int{6, 7, 10, 20, 21, 24, 25},
		},
		"clientOperations": clientOperations,
	}
}

// fixtureRoot is where the image keeps its fixtures.
var fixtureRoot = "/fixtures"

func printFixture(name string) int {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		logf("print-fixture: '%s' is not a fixture name", name)
		return exitUsage
	}
	raw, err := os.ReadFile(filepath.Join(fixtureRoot, name, "fixture.json"))
	if err != nil {
		logf("print-fixture: no fixture named '%s' in this image", name)
		return exitOperationFailed
	}
	_, _ = stdout.Write(raw)
	return exitOK
}

func run(argv []string) (int, error) {
	if len(argv) < 1 {
		fmt.Fprint(os.Stderr, usage)
		return exitUsage, nil
	}
	switch argv[0] {
	case "server":
		return runServer(&args{rest: argv[1:]})
	case "client":
		if len(argv) < 2 {
			fmt.Fprint(os.Stderr, usage)
			return exitUsage, nil
		}
		return runClient(argv[1], &args{rest: argv[2:]})
	case "print-capabilities":
		if len(argv) > 1 {
			return exitUsage, usagef("print-capabilities: unknown argument %s", argv[1])
		}
		emit(capabilities())
		return exitOK, nil
	case "print-fixture":
		name := "baseline"
		if len(argv) > 2 {
			return exitUsage, usagef("print-fixture: unknown argument %s", argv[2])
		}
		if len(argv) > 1 {
			name = argv[1]
		}
		return printFixture(name), nil
	case "version":
		fmt.Fprintf(stdout, "%s %s (otfabric/go-iec60870-5 %s %s, IEC 60870-5-103)\n", adapter, adapterVersion, upstreamVersion, upstreamRevision)
		return exitOK, nil
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
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
		if code == exitOK {
			code = exitOperationFailed
		}
	}
	os.Exit(code)
}
