// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
)

// usageError is anything the caller got wrong; it maps to exit code 2.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// args are command-line options of the form "--name value" and bare
// "--flag". Each accessor consumes what it reads; finish reports what is
// left as an unknown argument.
type args struct{ rest []string }

// str returns the value of flag name, or def when it is absent. A flag
// given twice is a usage error.
func (a *args) str(name, def string) (string, error) {
	val, found := def, false
	for i := 0; i < len(a.rest); {
		if a.rest[i] != name {
			i++
			continue
		}
		if i+1 >= len(a.rest) {
			return "", usagef("%s: a value is required", name)
		}
		if found {
			return "", usagef("%s: given more than once", name)
		}
		val, found = a.rest[i+1], true
		a.rest = append(a.rest[:i], a.rest[i+2:]...)
	}
	return val, nil
}

// has reports whether flag name is given with a value, without consuming it.
func (a *args) has(name string) bool {
	for _, v := range a.rest {
		if v == name {
			return true
		}
	}
	return false
}

// rng reads an integer flag that must lie in [lo, hi].
func (a *args) rng(name string, def, lo, hi int) (int, error) {
	s, err := a.str(name, "")
	if err != nil {
		return 0, err
	}
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, usagef("%s: '%s' is not an integer", name, s)
	}
	if n < lo || n > hi {
		return 0, usagef("%s: must be %d..%d, got %d", name, lo, hi, n)
	}
	return n, nil
}

// required reads an integer flag that must be given.
func (a *args) required(op, name string, lo, hi int) (int, error) {
	if !a.has(name) {
		return 0, usagef("%s: %s is required", op, name)
	}
	return a.rng(name, 0, lo, hi)
}

// flag consumes the switch name.
func (a *args) flag(name string) bool {
	found := false
	for i := 0; i < len(a.rest); {
		if a.rest[i] == name {
			a.rest = append(a.rest[:i], a.rest[i+1:]...)
			found = true
			continue
		}
		i++
	}
	return found
}

// finish fails when an argument was not consumed.
func (a *args) finish(context string) error {
	if len(a.rest) > 0 {
		return usagef("%s: unknown argument %s", context, a.rest[0])
	}
	return nil
}

// link reads the link flags of IEC 103: unbalanced only, a link address of
// one octet, 0..254 (255 is the broadcast address).
func (a *args) link() (int, error) {
	mode, err := a.str("--link-mode", "unbalanced")
	if err != nil {
		return 0, err
	}
	if mode != "unbalanced" {
		return 0, usagef("--link-mode: IEC 103 is unbalanced only, got %q", mode)
	}
	return a.rng("--link-address", 1, 0, 254)
}
