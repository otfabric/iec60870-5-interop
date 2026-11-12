// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-iec60870-5/iec102"
)

// flagSet holds "--name value" pairs. Every flag of IEC 102 takes a value;
// a flag given twice, a bare word or a flag without a value is a usage
// error, and so is any flag left over when parsing is done.
type flagSet struct{ m map[string]string }

func parseFlags(args []string) (*flagSet, error) {
	f := &flagSet{m: map[string]string{}}
	for i := 0; i < len(args); i += 2 {
		a := args[i]
		if !strings.HasPrefix(a, "--") || len(a) < 3 {
			return nil, fmt.Errorf("unexpected argument %q", a)
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("%s needs a value", a)
		}
		if _, dup := f.m[a]; dup {
			return nil, fmt.Errorf("%s given twice", a)
		}
		f.m[a] = args[i+1]
	}
	return f, nil
}

// str takes a flag; ok is false when it was not given.
func (f *flagSet) str(name string) (string, bool) {
	v, ok := f.m[name]
	delete(f.m, name)
	return v, ok
}

var integer = regexp.MustCompile(`^-?[0-9]+$`)

// intIn takes an integer flag within lo..hi, or def when it was not given.
func (f *flagSet) intIn(name string, def, lo, hi int64) (int64, bool, error) {
	s, ok := f.str(name)
	if !ok {
		return def, false, nil
	}
	if !integer.MatchString(s) {
		return 0, true, fmt.Errorf("%s: %q is not an integer", name, s)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < lo || n > hi {
		return 0, true, fmt.Errorf("%s: must be %d..%d, got %s", name, lo, hi, s)
	}
	return n, true, nil
}

// done fails on a flag nobody took: an unknown flag is never ignored.
func (f *flagSet) done() error {
	if len(f.m) == 0 {
		return nil
	}
	names := make([]string, 0, len(f.m))
	for k := range f.m {
		names = append(names, k)
	}
	slices.Sort(names)
	return fmt.Errorf("unknown flag %s", names[0])
}

// Profiles of the contract.
const (
	profileStandard = "standard"
	profileREE      = "ree"
)

// linkOpts are the link flags of IEC 102 (contract, "IEC 102 link").
type linkOpts struct {
	linkAddr     uint16
	linkAddrSize int
	ca           uint16
	caGiven      bool
	caSize       int
	profile      string
	key          uint32
}

// parseLink takes the link flags. The server has no --access-key: its
// fixture has the key, so for it the flag is left over and unknown.
func parseLink(f *flagSet, server bool) (linkOpts, error) {
	var o linkOpts
	if m, ok := f.str("--link-mode"); ok && m != "unbalanced" {
		return o, fmt.Errorf("--link-mode: IEC 102 is unbalanced only, got %q", m)
	}
	size, _, err := f.intIn("--link-address-size", 2, 1, 2)
	if err != nil {
		return o, err
	}
	o.linkAddrSize = int(size)
	maxLink := int64(65534)
	if size == 1 {
		maxLink = 254
	}
	la, _, err := f.intIn("--link-address", 1, 0, maxLink)
	if err != nil {
		return o, err
	}
	o.linkAddr = uint16(la)
	cs, _, err := f.intIn("--ca-size", 2, 1, 2)
	if err != nil {
		return o, err
	}
	o.caSize = int(cs)
	maxCA := int64(65535)
	if cs == 1 {
		maxCA = 255
	}
	ca, given, err := f.intIn("--common-address", 1, 0, maxCA)
	if err != nil {
		return o, err
	}
	o.ca, o.caGiven = uint16(ca), given
	o.profile = profileStandard
	if p, ok := f.str("--profile"); ok {
		if p != profileStandard && p != profileREE {
			return o, fmt.Errorf("--profile: %q or %q, got %q", profileStandard, profileREE, p)
		}
		o.profile = p
	}
	if server {
		return o, nil
	}
	key, given, err := f.intIn("--access-key", 0, 0, 4294967295)
	if err != nil {
		return o, err
	}
	switch {
	case o.profile == profileREE && !given:
		return o, fmt.Errorf("--access-key is required with --profile ree")
	case o.profile != profileREE && given:
		return o, fmt.Errorf("--access-key needs --profile ree")
	}
	o.key = uint32(key)
	return o, nil
}

// params are the codec parameters of a run.
func (o linkOpts) params(p iec102.Profile) iec102.Params {
	return iec102.Params{CommonAddrSize: o.caSize, Location: time.UTC, Profile: p}
}

// serverOpts are the flags of "server".
type serverOpts struct {
	fixture   string
	bind      string
	port      int
	readyFile string
	link      linkOpts
}

func parseServer(args []string) (serverOpts, error) {
	o := serverOpts{fixture: defaultFixture, bind: "0.0.0.0", port: defaultPort, readyFile: defaultReadyFile}
	f, err := parseFlags(args)
	if err != nil {
		return o, err
	}
	if v, ok := f.str("--fixture"); ok {
		o.fixture = v
	}
	if v, ok := f.str("--bind-address"); ok {
		o.bind = v
	}
	if v, ok := f.str("--ready-file"); ok {
		o.readyFile = v
	}
	port, _, err := f.intIn("--port", defaultPort, 0, 65535)
	if err != nil {
		return o, err
	}
	o.port = int(port)
	if o.link, err = parseLink(f, true); err != nil {
		return o, err
	}
	return o, f.done()
}

// Client operations of IEC 102 (contract, "IEC 102 client").
const (
	opConnect     = "connect"
	opReadTotals  = "read-totals"
	opReadTime    = "read-time"
	opSetTime     = "set-time"
	opReadProduct = "read-product"
	opReadTariff  = "read-tariff"
)

var operations = []string{opConnect, opReadTotals, opReadTime, opSetTime, opReadProduct, opReadTariff}

// clientOpts are the flags of "client".
type clientOpts struct {
	op             string
	host           string
	port           int
	link           linkOpts
	connectTimeout time.Duration
	timeout        time.Duration

	kind        string // read-totals: absolute or incremental
	record      uint8  // read-totals: record address; read-tariff: register
	first, last uint8
	from, to    iec102.TimeA
	ranged      bool // read-tariff: --from and --to given
	time        iec102.TimeB
}

func parseClient(args []string) (clientOpts, error) {
	var o clientOpts
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return o, fmt.Errorf("client: the operation is missing")
	}
	o.op = args[0]
	if !slices.Contains(operations, o.op) {
		return o, fmt.Errorf("client: unknown operation %q", o.op)
	}
	f, err := parseFlags(args[1:])
	if err != nil {
		return o, err
	}
	var ok bool
	if o.host, ok = f.str("--host"); !ok || o.host == "" {
		return o, fmt.Errorf("--host is required")
	}
	port, _, err := f.intIn("--port", defaultPort, 1, 65535)
	if err != nil {
		return o, err
	}
	o.port = int(port)
	ms, _, err := f.intIn("--connect-timeout-ms", 5000, 1, 3600000)
	if err != nil {
		return o, err
	}
	o.connectTimeout = time.Duration(ms) * time.Millisecond
	if ms, _, err = f.intIn("--timeout-ms", 5000, 1, 3600000); err != nil {
		return o, err
	}
	o.timeout = time.Duration(ms) * time.Millisecond
	if o.link, err = parseLink(f, false); err != nil {
		return o, err
	}
	if (o.op == opSetTime || o.op == opReadTariff) && o.link.profile != profileREE {
		return o, fmt.Errorf("%s is an operation of the REE profile: give --profile ree", o.op)
	}
	switch o.op {
	case opReadTotals:
		err = o.parseTotals(f)
	case opSetTime:
		s, _ := f.str("--time")
		o.time, err = parseTimeB("--time", s)
	case opReadTariff:
		err = o.parseTariff(f)
	}
	if err != nil {
		return o, err
	}
	return o, f.done()
}

func (o *clientOpts) parseTotals(f *flagSet) error {
	o.kind = "absolute"
	if k, ok := f.str("--kind"); ok {
		if k != "absolute" && k != "incremental" {
			return fmt.Errorf("--kind: \"absolute\" or \"incremental\", got %q", k)
		}
		o.kind = k
	}
	r, _, err := f.intIn("--record", 11, 0, 255)
	if err != nil {
		return err
	}
	first, _, err := f.intIn("--first", 1, 1, 255)
	if err != nil {
		return err
	}
	last, _, err := f.intIn("--last", 8, 1, 255)
	if err != nil {
		return err
	}
	if first > last {
		return fmt.Errorf("--first is larger than --last")
	}
	o.record, o.first, o.last = uint8(r), uint8(first), uint8(last)
	from, _ := f.str("--from")
	to, _ := f.str("--to")
	return o.parseRange(from, to)
}

func (o *clientOpts) parseTariff(f *flagSet) error {
	r, _, err := f.intIn("--register", 134, 0, 255)
	if err != nil {
		return err
	}
	o.record = uint8(r)
	from, okFrom := f.str("--from")
	to, okTo := f.str("--to")
	if okFrom != okTo {
		return fmt.Errorf("read-tariff: --from and --to go together")
	}
	if !okFrom {
		return nil
	}
	o.ranged = true
	return o.parseRange(from, to)
}

func (o *clientOpts) parseRange(from, to string) error {
	var err error
	if o.from, err = parseTimeA("--from", from); err != nil {
		return err
	}
	if o.to, err = parseTimeA("--to", to); err != nil {
		return err
	}
	if textA(o.from) > textA(o.to) {
		return fmt.Errorf("--from is later than --to")
	}
	return nil
}

// Time formats of the contract: the station's local time without a zone.
const (
	layoutA = "2006-01-02T15:04"
	layoutB = "2006-01-02T15:04:05.000"
)

var (
	timeAText = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}$`)
	timeBText = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}$`)
)

// localTime parses a time of the contract as wall-clock fields.
func localTime(name, s string, re *regexp.Regexp, layout string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("%s is required", name)
	}
	if !re.MatchString(s) {
		return time.Time{}, fmt.Errorf("%s: %q is not a local time %s", name, s, layout)
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", name, err)
	}
	if t.Year() < 2000 || t.Year() > 2099 {
		return time.Time{}, fmt.Errorf("%s: the year must be 2000..2099", name)
	}
	return t, nil
}

// parseTimeA parses a time a. The fields are the wall clock as written;
// SU is 0 (the location is UTC, which has no summer time): this is how the
// adapter writes the SU bit of every time it sends.
func parseTimeA(name, s string) (iec102.TimeA, error) {
	t, err := localTime(name, s, timeAText, layoutA)
	if err != nil {
		return iec102.TimeA{}, err
	}
	return iec102.TimeAOf(t, time.UTC), nil
}

// parseTimeB parses a time b, SU 0 as for parseTimeA.
func parseTimeB(name, s string) (iec102.TimeB, error) {
	t, err := localTime(name, s, timeBText, layoutB)
	if err != nil {
		return iec102.TimeB{}, err
	}
	return iec102.TimeBOf(t, time.UTC), nil
}

// textA writes a time a of the contract (the date and time fields only).
func textA(t iec102.TimeA) string {
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d", 2000+int(t.Year), t.Month, t.Day, t.Hour, t.Minute)
}

// textB writes a time b of the contract.
func textB(t iec102.TimeB) string {
	return fmt.Sprintf("%s:%02d.%03d", textA(t.TimeA), t.Second, t.Millisecond)
}
