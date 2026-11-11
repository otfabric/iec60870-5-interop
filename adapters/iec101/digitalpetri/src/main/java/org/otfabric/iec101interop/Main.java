// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * The IEC 60870-5-101 adapter of iec60870-5-interop for digitalpetri/iec60870: an
 * IEC 101 controlled and controlling station whose FT 1.2 byte stream runs over
 * one TCP connection, as through a terminal server (the library's own
 * TcpIec101Client and TcpIec101Server).
 *
 * <p>It implements the container contract in docs/CONTAINER_CONTRACT.md.
 */
public final class Main {
    static final String ADAPTER = "digitalpetri";
    static final String SCHEMA_VERSION = "1.0";
    static final String STANDARD = "iec60870-5-101";
    static final String DEFAULT_FIXTURE = "/fixtures/baseline/fixture.json";
    static final String DEFAULT_READY_FILE = "/run/iec60870-5-interop/ready";
    static final int DEFAULT_PORT = 2401;

    static final int EXIT_OK = 0;
    static final int EXIT_OPERATION_FAILED = 1;
    static final int EXIT_USAGE = 2;
    static final int EXIT_CONNECT_FAILED = 3;
    static final int EXIT_FIXTURE_INVALID = 4;

    private Main() {}

    private static String env(String name, String def) {
        String v = System.getenv(name);
        return v == null || v.isEmpty() ? def : v;
    }

    static final String ADAPTER_VERSION = env("ADAPTER_VERSION", "dev");
    static final String UPSTREAM_VERSION = env("UPSTREAM_VERSION", "unknown");
    static final String UPSTREAM_REVISION = env("UPSTREAM_REVISION", "unknown");

    private static final String USAGE = """
            usage: iec60870-5-interop <command>

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
            """;

    static int printCapabilities() {
        Map<String, Object> upstream = new LinkedHashMap<>();
        upstream.put("name", "digitalpetri/iec60870");
        upstream.put("version", UPSTREAM_VERSION);
        upstream.put("revision", UPSTREAM_REVISION);
        upstream.put("license", "EPL-2.0");
        upstream.put("language", "Java");
        upstream.put("independentEngine", true);

        Map<String, Object> roles = new LinkedHashMap<>();
        roles.put("server", true);
        roles.put("client", true);

        Map<String, Object> f = new LinkedHashMap<>();
        f.put("generalInterrogation", true);
        f.put("groupInterrogation", true);
        f.put("counterInterrogation", true);
        f.put("read", true);
        f.put("clockSync", true);
        f.put("testCommand", true);
        f.put("directCommands", true);
        f.put("selectBeforeOperate", true);
        f.put("commandDeactivation", true);
        f.put("timeTaggedCommands", false);
        f.put("spontaneousOnCommand", true);
        f.put("apciParameters", false);
        f.put("multipleConnections", false);
        f.put("pointQualityOnBitstring", true);
        f.put("tls", false);
        f.put("tlsServer", false);
        f.put("tlsClient", false);
        f.put("dataTransferEvents", false);
        f.put("fileTransfer", false);
        f.put("fileServer", false);
        f.put("fileClient", false);
        f.put("fileUploadServer", false);
        f.put("fileUploadClient", false);
        f.put("fileDirectoryServer", false);
        f.put("fileDirectoryClient", false);
        f.put("redundancyServer", false);
        f.put("redundancyClient", false);
        f.put("secureAuthentication", false);
        f.put("unbalancedMode", true);
        f.put("balancedMode", true);
        f.put("distinctBalancedAddresses", false);
        // Contract 2.3.
        // Its unbalanced master addresses the station whose link address equals
        // the ASDU's common address.
        f.put("unbalancedCommonAddress", false);

        Map<String, Object> caps = new LinkedHashMap<>();
        caps.put("schemaVersion", SCHEMA_VERSION);
        caps.put("adapter", ADAPTER);
        caps.put("adapterVersion", ADAPTER_VERSION);
        caps.put("protocol", STANDARD);
        caps.put("standard", STANDARD);
        caps.put("transport", "tcp-stream");
        caps.put("linkModes", List.of("unbalanced", "balanced"));
        caps.put("linkAddressSizes", List.of(0, 1, 2));
        caps.put("cotSizes", List.of(1, 2));
        caps.put("caSizes", List.of(1, 2));
        caps.put("ioaSizes", List.of(1, 2, 3));
        caps.put("upstream", upstream);
        caps.put("roles", roles);
        caps.put("features", f);
        caps.put("pointTypes", List.of("M_SP_NA_1", "M_DP_NA_1", "M_ST_NA_1", "M_BO_NA_1", "M_ME_NA_1",
                "M_ME_NB_1", "M_ME_NC_1", "M_IT_NA_1"));
        caps.put("commandTypes", List.of("C_SC_NA_1", "C_DC_NA_1", "C_RC_NA_1", "C_SE_NA_1", "C_SE_NB_1",
                "C_SE_NC_1"));
        caps.put("clientOperations", List.of("connect", "interrogate", "counter-interrogate", "read",
                "clock-sync", "test-command", "command", "monitor"));
        Codec.emit(caps);
        return EXIT_OK;
    }

    static int printFixture(String name) {
        if (name.contains("/") || name.contains("\\") || name.contains("..")) {
            Codec.log("print-fixture: '%s' is not a fixture name", name);
            return EXIT_USAGE;
        }
        try {
            byte[] raw = Files.readAllBytes(Path.of("/fixtures", name, "fixture.json"));
            System.out.write(raw);
            System.out.flush();
            return EXIT_OK;
        } catch (java.io.IOException e) {
            Codec.log("print-fixture: no fixture named '%s' in this image", name);
            return EXIT_OPERATION_FAILED;
        }
    }

    static int run(String[] argv) throws Exception {
        if (argv.length < 1) {
            System.err.print(USAGE);
            return EXIT_USAGE;
        }
        switch (argv[0]) {
            case "server":
                return ServerCommand.run(new Args(argv, 1));
            case "client":
                if (argv.length < 2) {
                    System.err.print(USAGE);
                    return EXIT_USAGE;
                }
                return ClientCommand.run(argv[1], new Args(argv, 2));
            case "print-capabilities":
                return printCapabilities();
            case "print-fixture":
                return printFixture(argv.length > 1 ? argv[1] : "baseline");
            case "version":
                System.out.printf("%s %s (digitalpetri/iec60870 %s, IEC 60870-5-101)%n", ADAPTER, ADAPTER_VERSION,
                        UPSTREAM_VERSION);
                return EXIT_OK;
            case "help":
            case "--help":
            case "-h":
                System.out.print(USAGE);
                return EXIT_OK;
            default:
                System.err.print(USAGE);
                return EXIT_USAGE;
        }
    }

    public static void main(String[] argv) {
        int code;
        try {
            code = run(argv);
        } catch (Args.UsageException e) {
            Codec.log("%s", e.getMessage());
            code = EXIT_USAGE;
        } catch (Exception e) {
            Codec.log("fatal: %s", e);
            code = EXIT_OPERATION_FAILED;
        }
        // Netty keeps non-daemon threads; exit explicitly with the contract's code.
        System.exit(code);
    }
}
