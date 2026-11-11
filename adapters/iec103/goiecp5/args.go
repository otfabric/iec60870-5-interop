// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"strconv"
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

// rng reads an integer flag that must lie in [lo, hi].
func (a *args) rng(name string, def, lo, hi int) (int, error) {
	n, err := a.num(name, def)
	if err != nil {
		return 0, err
	}
	if n < lo || n > hi {
		return 0, usagef("%s: must be %d..%d, got %d", name, lo, hi, n)
	}
	return n, nil
}

func (a *args) flag(name string) bool {
	for i, v := range a.rest {
		if v == name {
			a.rest = append(a.rest[:i], a.rest[i+1:]...)
			return true
		}
	}
	return false
}

// finish fails when an argument was not consumed.
func (a *args) finish(context string) error {
	if len(a.rest) > 0 {
		return usagef("%s: unknown argument %s", context, a.rest[0])
	}
	return nil
}

// link reads the link flags of IEC 103: unbalanced only, a link address of
// one octet (255 is the broadcast address).
func (a *args) link() (int, error) {
	if mode := a.str("--link-mode", "unbalanced"); mode != "unbalanced" {
		return 0, usagef("--link-mode: IEC 103 is unbalanced only, got %q", mode)
	}
	return a.rng("--link-address", 1, 0, 254)
}
