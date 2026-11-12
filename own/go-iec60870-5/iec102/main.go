// SPDX-License-Identifier: MIT

// Command iec60870-5-interop is OT Fabric's own IEC 60870-5-102 stack
// (github.com/otfabric/go-iec60870-5: packages iec102, iec102/client,
// iec102/server, iec102/ree, link and ft12) behind the container contract of
// iec60870-5-interop (docs/CONTAINER_CONTRACT.md, "IEC 102"): the integrated
// total station serving an IEC 102 fixture (server) and the controlling
// station (client), over the FT 1.2 byte stream on one TCP connection.
//
// Not an independent implementation: upstream.independentEngine is false.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"syscall"
)

// Set with -ldflags -X at build time.
var (
	adapterVersion   = "dev"
	upstreamVersion  = "unknown"
	upstreamRevision = "unknown"
)

const (
	adapterName      = "otfabric"
	standardName     = "iec60870-5-102"
	schemaVersion    = "1.0"
	fixturesDir      = "/fixtures"
	defaultFixture   = "/fixtures/baseline/fixture.json"
	defaultReadyFile = "/run/iec60870-5-interop/ready"
	defaultPort      = 2401
)

// Exit codes of the contract.
const (
	exitOK      = 0
	exitFailed  = 1
	exitUsage   = 2
	exitConnect = 3
	exitFixture = 4
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one command and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return usage(stderr, fmt.Errorf("a command is missing"))
	}
	switch args[0] {
	case "server":
		o, err := parseServer(args[1:])
		if err != nil {
			return usage(stderr, err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		return runServer(ctx, o, stdout, stderr)
	case "client":
		o, err := parseClient(args[1:])
		if err != nil {
			return usage(stderr, err)
		}
		return runClient(o, stdout, stderr)
	case "print-capabilities":
		if len(args) > 1 {
			return usage(stderr, fmt.Errorf("print-capabilities takes no arguments"))
		}
		writeJSONLine(stdout, capabilities())
		return exitOK
	case "print-fixture":
		return printFixture(args[1:], stdout, stderr)
	case "version":
		_, _ = fmt.Fprintf(stdout, "%s %s (go-iec60870-5 %s %s, IEC 60870-5-102)\n",
			adapterName, adapterVersion, upstreamVersion, upstreamRevision)
		return exitOK
	}
	return usage(stderr, fmt.Errorf("unknown command %q", args[0]))
}

func usage(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "[otfabric] usage: %v\n", err)
	return exitUsage
}

var fixtureName = regexp.MustCompile(`^[a-z0-9-]+$`)

// printFixture writes a baked fixture byte for byte.
func printFixture(args []string, stdout, stderr io.Writer) int {
	name := "baseline"
	if len(args) > 0 {
		name = args[0]
	}
	if len(args) > 1 || !fixtureName.MatchString(name) {
		return usage(stderr, fmt.Errorf("print-fixture: %q is not a fixture name", name))
	}
	data, err := os.ReadFile(fixturesDir + "/" + name + "/fixture.json")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "[otfabric] print-fixture: no fixture named %q in this image\n", name)
		return exitFailed
	}
	_, _ = stdout.Write(data)
	return exitOK
}
