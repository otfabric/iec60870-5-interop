// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import java.util.ArrayList;
import java.util.List;

/** Command-line options of the form {@code --name value} and bare {@code --flag}. */
final class Args {
    /** Anything the caller got wrong; maps to exit code 2. */
    static final class UsageException extends RuntimeException {
        private static final long serialVersionUID = 1L;

        UsageException(String message) {
            super(message);
        }
    }

    static UsageException usage(String format, Object... args) {
        return new UsageException(String.format(format, args));
    }

    private final List<String> rest;

    Args(String[] argv, int from) {
        rest = new ArrayList<>();
        for (int i = from; i < argv.length; i++) {
            rest.add(argv[i]);
        }
    }

    String string(String name, String def) {
        int i = rest.indexOf(name);
        if (i < 0 || i + 1 >= rest.size()) {
            return def;
        }
        String v = rest.get(i + 1);
        rest.remove(i + 1);
        rest.remove(i);
        return v;
    }

    int integer(String name, int def) {
        String s = string(name, null);
        if (s == null) {
            return def;
        }
        try {
            return Integer.parseInt(s);
        } catch (NumberFormatException e) {
            throw usage("%s: '%s' is not an integer", name, s);
        }
    }

    boolean flag(String name) {
        return rest.remove(name);
    }

    /** Fails when an argument was not consumed. */
    void finish(String context) {
        if (!rest.isEmpty()) {
            throw usage("%s: unknown argument %s", context, rest.get(0));
        }
    }
}
