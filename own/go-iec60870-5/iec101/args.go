// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/otfabric/go-iec60870-5/asdu"
	"github.com/otfabric/go-iec60870-5/ft12"
	"github.com/otfabric/go-iec60870-5/iec101"
)

// usageError is anything the caller got wrong; it maps to exit code 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// args are command-line options of the form "--name value" and bare "--flag".
type args struct{ rest []string }

func (a *args) str(name, def string) string {
	for i, v := range a.rest {
		if v == name && i+1 < len(a.rest) {
			val := a.rest[i+1]
			a.rest = append(a.rest[:i], a.rest[i+2:]...)
			return val
		}
	}
	return def
}

func (a *args) num(name string, def int) (int, error) {
	s := a.str(name, "")
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, usagef("%s: '%s' is not an integer", name, s)
	}
	return n, nil
}

// finish fails when an argument was not consumed.
func (a *args) finish(context string) error {
	if len(a.rest) > 0 {
		return usagef("%s: unknown argument %s", context, a.rest[0])
	}
	return nil
}

// linkConfig is the IEC 101 link and the field sizes of the contract's
// link flags.
type linkConfig struct {
	balanced   bool
	address    int // the controlled station's link address
	remote     int // balanced: the controlling station's link address
	addrSize   int
	cotSize    int
	caSize     int
	ioaSize    int
	commonAddr int // --common-address, 0 when not given
}

// link reads the link flags of the contract. Every size the contract has is
// one go-iec60870-5 takes (ft12.Params.Validate, asdu.Params.Validate).
func (a *args) link() (linkConfig, error) {
	var c linkConfig
	mode := a.str("--link-mode", "unbalanced")
	switch mode {
	case "unbalanced":
	case "balanced":
		c.balanced = true
	default:
		return c, usagef("--link-mode: must be unbalanced or balanced, got %q", mode)
	}
	var err error
	if c.addrSize, err = a.num("--link-address-size", 1); err != nil {
		return c, err
	}
	if c.address, err = a.num("--link-address", 1); err != nil {
		return c, err
	}
	remote := a.str("--remote-link-address", "")
	if c.cotSize, err = a.num("--cot-size", 1); err != nil {
		return c, err
	}
	if c.caSize, err = a.num("--ca-size", 1); err != nil {
		return c, err
	}
	if c.ioaSize, err = a.num("--ioa-size", 2); err != nil {
		return c, err
	}
	if c.commonAddr, err = a.num("--common-address", 0); err != nil {
		return c, err
	}
	switch {
	case c.addrSize < 0 || c.addrSize > 2:
		return c, usagef("--link-address-size: must be 0, 1 or 2")
	case c.addrSize == 0 && !c.balanced:
		return c, usagef("--link-address-size 0 is for balanced mode only")
	case c.cotSize < 1 || c.cotSize > 2:
		return c, usagef("--cot-size: must be 1 or 2")
	case c.caSize < 1 || c.caSize > 2:
		return c, usagef("--ca-size: must be 1 or 2")
	case c.ioaSize < 1 || c.ioaSize > 3:
		return c, usagef("--ioa-size: must be 1, 2 or 3")
	}
	maxAddr := 0
	if c.addrSize > 0 {
		maxAddr = 1<<(8*c.addrSize) - 2 // all ones is the broadcast address
	}
	if c.address < 0 || c.address > maxAddr {
		return c, usagef("--link-address: must be 0..%d for a link address of %d octets", maxAddr, c.addrSize)
	}
	c.remote = c.address
	if remote != "" {
		if !c.balanced {
			return c, usagef("--remote-link-address is for balanced mode only")
		}
		n, err := strconv.Atoi(remote)
		if err != nil {
			return c, usagef("--remote-link-address: '%s' is not an integer", remote)
		}
		if n < 0 || n > maxAddr {
			return c, usagef("--remote-link-address: must be 0..%d", maxAddr)
		}
		c.remote = n
	}
	maxCA := 1<<(8*c.caSize) - 1
	if c.commonAddr < 0 || c.commonAddr > maxCA {
		return c, usagef("--common-address: must be 1..%d for a common address of %d octets", maxCA, c.caSize)
	}
	return c, nil
}

// linkTimeout is the answer timeout of a primary: far shorter on a TCP
// stream than the bit-time formula of a serial line; the retries cover a
// slow peer (CONTAINER_CONTRACT.md, transport).
const linkTimeout = 500 * time.Millisecond

// params are the go-iec60870-5 system parameters of the link flags.
func (c linkConfig) params() iec101.Params {
	p := iec101.DefaultParams()
	if c.balanced {
		p.Mode = iec101.ModeBalanced
	}
	p.Link.FT12 = ft12.Params{AddrSize: c.addrSize}
	p.Link.Timeout = linkTimeout
	p.ASDU = asdu.Params{CauseSize: c.cotSize, CommonAddrSize: c.caSize, IOASize: c.ioaSize}
	return p
}

// asduParams are the ASDU field sizes of the link flags.
func (c linkConfig) asduParams() asdu.Params {
	return asdu.Params{CauseSize: c.cotSize, CommonAddrSize: c.caSize, IOASize: c.ioaSize}
}

func (c linkConfig) maxIOA() int { return 1<<(8*c.ioaSize) - 1 }

func (c linkConfig) modeName() string {
	if c.balanced {
		return "balanced"
	}
	return "unbalanced"
}
