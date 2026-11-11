// SPDX-License-Identifier: GPL-3.0-or-later

// Command iec60870-5-interop is the IEC 60870-5-101 adapter of
// iec60870-5-interop for cB-Sigma/iec60870 (package cs101): an IEC 101
// controlled and controlling station whose FT 1.2 byte stream runs over one
// TCP connection, as through a terminal server.
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
	adapter          = "cbsigma"
	schemaVersion    = "1.0"
	standard         = "iec60870-5-101"
	defaultFixture   = "/fixtures/baseline/fixture.json"
	defaultReadyFile = "/run/iec60870-5-interop/ready"
	defaultPort      = 2401

	exitOK              = 0
	exitOperationFailed = 1
	exitUsage           = 2
	exitConnectFailed   = 3
	exitFixtureInvalid  = 4
)

// Set by the linker; see the Dockerfile.
var (
	adapterVersion  = "dev"
	upstreamVersion = "unknown" // cB-Sigma/iec60870
)

const usage = `usage: iec60870-5-interop <command>

  server [--fixture PATH] [--bind-address ADDR] [--port N] [--ready-file PATH] [--common-address N] [LINK]
  client <operation> --host HOST [--port N] [--common-address N] [--originator-address N]
         [--timeout-ms N] [--connect-timeout-ms N] [--collect-ms N] [LINK] [operation flags]
      connect             [--hold-ms N]
      interrogate         [--qoi N]
      counter-interrogate [--qcc N]
      read                --ioa N
      clock-sync          [--time 2026-01-02T03:04:05.678Z]
      test-command
      command             --type T --ioa N --value V [--mode direct|select|sbo|cancel] [--qualifier N]
      monitor             [--duration-ms N] [--max-asdus N]
  print-capabilities
  print-fixture [NAME]
  version

  LINK: [--link-mode unbalanced|balanced] [--link-address N] [--remote-link-address N]
        [--link-address-size 0|1|2] [--cot-size 1|2] [--ca-size 1|2] [--ioa-size 1|2|3]

See docs/CONTAINER_CONTRACT.md in https://github.com/otfabric/iec60870-5-interop
`

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
		"linkModes":      []string{"unbalanced", "balanced"},
		"upstream": object{
			"name":     "cB-Sigma/iec60870",
			"version":  upstreamVersion,
			"revision": "github.com/cB-Sigma/iec60870@" + upstreamVersion,
			"license":  "Apache-2.0",
			"language": "Go",
			// Written from the IEC documents; its IEC 101 link was debugged
			// against lib60870-C, so it is not independent evidence for that
			// pairing on its own (docs/REFERENCE_STACKS.md).
			"independentEngine": true,
		},
		"roles": object{"server": true, "client": true},
		"features": object{
			"generalInterrogation":      true,
			"groupInterrogation":        true,
			"counterInterrogation":      true,
			"read":                      true,
			"clockSync":                 true,
			"testCommand":               true,
			"directCommands":            true,
			"selectBeforeOperate":       true,
			"commandDeactivation":       true,
			"timeTaggedCommands":        false,
			"spontaneousOnCommand":      true,
			"apciParameters":            false,
			"multipleConnections":       false,
			"pointQualityOnBitstring":   true,
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
			"balancedMode":              true,
			"distinctBalancedAddresses": false,
		},
		"pointTypes": []string{"M_SP_NA_1", "M_DP_NA_1", "M_ST_NA_1", "M_BO_NA_1", "M_ME_NA_1", "M_ME_NB_1",
			"M_ME_NC_1", "M_IT_NA_1"},
		"commandTypes": []string{"C_SC_NA_1", "C_DC_NA_1", "C_RC_NA_1", "C_SE_NA_1", "C_SE_NB_1", "C_SE_NC_1"},
		"clientOperations": []string{"connect", "interrogate", "counter-interrogate", "read", "clock-sync",
			"test-command", "command", "monitor"},
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
		return runServer(&args{rest: argv[1:]})
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
		fmt.Printf("%s %s (cB-Sigma/iec60870 %s, IEC 60870-5-101)\n", adapter, adapterVersion, upstreamVersion)
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
