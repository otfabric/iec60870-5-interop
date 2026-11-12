// SPDX-License-Identifier: MIT

// Command iec60870-5-interop is OT Fabric's own IEC 60870-5-101 stack,
// go-iec60870-5 (packages iec101/client and iec101/server), behind the
// container contract of iec60870-5-interop: an IEC 101 controlled and
// controlling station whose FT 1.2 byte stream runs over one TCP
// connection, as through a terminal server.
//
// It implements the container contract in docs/CONTAINER_CONTRACT.md. It is
// not an independent implementation (upstream.independentEngine false).
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
	adapter          = "otfabric"
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

// Set by the linker; see own/go-iec60870-5/Dockerfile.
var (
	adapterVersion   = "dev"
	upstreamVersion  = "unknown" // the go-iec60870-5 release
	upstreamRevision = "unknown" // its commit
)

// fixturesDir is where the image's fixtures are; a variable for the tests.
var fixturesDir = "/fixtures"

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

var (
	outMu sync.Mutex
	// stdout is where the JSON documents go; a variable for the tests.
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

// stderrLogger passes the library's warnings and errors to stderr.
type stderrLogger struct{}

func (stderrLogger) Debugf(string, ...any)          {}
func (stderrLogger) Infof(string, ...any)           {}
func (stderrLogger) Warnf(format string, a ...any)  { logf("warn: "+format, a...) }
func (stderrLogger) Errorf(format string, a ...any) { logf("error: "+format, a...) }

func capabilities() object {
	return object{
		"schemaVersion":  schemaVersion,
		"adapter":        adapter,
		"adapterVersion": adapterVersion,
		"protocol":       standard,
		"standard":       standard,
		"transport":      "tcp-stream",
		"linkModes":      []string{"unbalanced", "balanced"},
		// Contract 2.3: every field size go-iec60870-5 takes
		// (ft12.Params.Validate: link address 0, 1, 2; asdu.Params.Validate:
		// cause 1, 2, common address 1, 2, object address 1..3).
		"linkAddressSizes": []int{0, 1, 2},
		"cotSizes":         []int{1, 2},
		"caSizes":          []int{1, 2},
		"ioaSizes":         []int{1, 2, 3},
		"upstream": object{
			"name":     "otfabric/go-iec60870-5",
			"version":  upstreamVersion,
			"revision": upstreamRevision,
			"license":  "MIT",
			"language": "Go",
			// OT Fabric's own stack: never additional independent evidence.
			"independentEngine": false,
		},
		"roles": object{"server": true, "client": true},
		"features": object{
			"generalInterrogation":    true,
			"groupInterrogation":      true,
			"counterInterrogation":    true,
			"read":                    true,
			"clockSync":               true,
			"testCommand":             true,
			"directCommands":          true,
			"selectBeforeOperate":     true,
			"commandDeactivation":     true,
			"timeTaggedCommands":      false,
			"spontaneousOnCommand":    true,
			"apciParameters":          false,
			"multipleConnections":     false,
			"pointQualityOnBitstring": true,
			"tls":                     false,
			"tlsServer":               false,
			"tlsClient":               false,
			"dataTransferEvents":      false,
			"fileTransfer":            false,
			"fileServer":              false,
			"fileClient":              false,
			"fileUploadServer":        false,
			"fileUploadClient":        false,
			"fileDirectoryServer":     false,
			"fileDirectoryClient":     false,
			"redundancyServer":        false,
			"redundancyClient":        false,
			"secureAuthentication":    false,
			"unbalancedMode":          true,
			"balancedMode":            true,
			// link.BalancedConfig has the station's own address and the
			// peer's: the controlled station sends its primary messages to
			// --remote-link-address.
			"distinctBalancedAddresses": true,
			// iec101/client polls and sends to Config.LinkAddr whatever the
			// common address of the ASDU.
			"unbalancedCommonAddress": true,
		},
		"pointTypes": []string{"M_SP_NA_1", "M_DP_NA_1", "M_ST_NA_1", "M_BO_NA_1", "M_ME_NA_1", "M_ME_NB_1",
			"M_ME_NC_1", "M_IT_NA_1"},
		"commandTypes": []string{"C_SC_NA_1", "C_DC_NA_1", "C_RC_NA_1", "C_SE_NA_1", "C_SE_NB_1", "C_SE_NC_1"},
		"clientOperations": []string{"connect", "interrogate", "counter-interrogate", "read", "clock-sync",
			"test-command", "command", "monitor"},
	}
}

func printFixture(name string) int {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		logf("print-fixture: '%s' is not a fixture name", name)
		return exitOperationFailed
	}
	raw, err := os.ReadFile(filepath.Join(fixturesDir, name, "fixture.json"))
	if err != nil {
		logf("print-fixture: no fixture named '%s' in this image", name)
		return exitOperationFailed
	}
	outMu.Lock()
	_, _ = stdout.Write(raw)
	outMu.Unlock()
	return exitOK
}

func run(argv []string) (int, error) {
	if len(argv) < 1 {
		fmt.Fprint(os.Stderr, usage)
		return exitUsage, nil
	}
	switch argv[0] {
	case "server":
		return runServer(&args{rest: argv[1:]}, nil)
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
		fmt.Fprintf(stdout, "%s %s (otfabric/go-iec60870-5 %s %s, IEC 60870-5-101)\n", adapter, adapterVersion,
			upstreamVersion, upstreamRevision)
		return exitOK, nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return exitOK, nil
	}
	fmt.Fprint(os.Stderr, usage)
	return exitUsage, nil
}

// exitCode maps the result of run to the exit code, logging the error.
func exitCode(code int, err error) int {
	var ue usageError
	switch {
	case errors.As(err, &ue):
		logf("%s", ue.msg)
		return exitUsage
	case err != nil:
		logf("fatal: %v", err)
		return exitOperationFailed
	}
	return code
}

func main() {
	os.Exit(exitCode(run(os.Args[1:])))
}
