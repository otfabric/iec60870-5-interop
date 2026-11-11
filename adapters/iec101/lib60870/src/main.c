/* SPDX-License-Identifier: GPL-3.0-or-later */
#include "common.h"

#include <stdio.h>
#include <string.h>

static const char USAGE[] =
    "usage: iec60870-5-interop <command>\n"
    "\n"
    "  server [--fixture PATH] [--bind-address ADDR] [--port N] [--ready-file PATH] [--common-address N] [LINK]\n"
    "  client <operation> --host HOST [--port N] [--common-address N] [--originator-address N]\n"
    "         [--timeout-ms N] [--connect-timeout-ms N] [--collect-ms N] [LINK] [operation flags]\n"
    "      connect             [--hold-ms N]\n"
    "      interrogate         [--qoi N]\n"
    "      counter-interrogate [--qcc N]\n"
    "      read                --ioa N\n"
    "      clock-sync          [--time 2026-01-02T03:04:05.678Z]\n"
    "      test-command\n"
    "      command             --type T --ioa N --value V [--mode direct|select|sbo|cancel] [--qualifier N]\n"
    "      monitor             [--duration-ms N] [--max-asdus N]\n"
    "  print-capabilities\n"
    "  print-fixture [NAME]\n"
    "  version\n"
    "\n"
    "  LINK: [--link-mode unbalanced|balanced] [--link-address N] [--remote-link-address N]\n"
    "        [--link-address-size 0|1|2] [--cot-size 1|2] [--ca-size 1|2] [--ioa-size 1|2|3]\n"
    "\n"
    "See docs/CONTAINER_CONTRACT.md in https://github.com/otfabric/iec60870-5-interop\n";

int printCapabilities(void)
{
    static const char* pointTypes[] = { "M_SP_NA_1", "M_DP_NA_1", "M_ST_NA_1", "M_BO_NA_1", "M_ME_NA_1", "M_ME_NB_1",
        "M_ME_NC_1", "M_IT_NA_1" };
    static const char* commandTypes[] = { "C_SC_NA_1", "C_DC_NA_1", "C_RC_NA_1", "C_SE_NA_1", "C_SE_NB_1",
        "C_SE_NC_1" };
    static const char* operations[] = { "connect", "interrogate", "counter-interrogate", "read", "clock-sync",
        "test-command", "command", "monitor" };
    static const char* modes[] = { "unbalanced", "balanced" };

    cJSON* c = cJSON_CreateObject();
    cJSON_AddStringToObject(c, "schemaVersion", SCHEMA_VERSION);
    cJSON_AddStringToObject(c, "adapter", ADAPTER_NAME);
    cJSON_AddStringToObject(c, "adapterVersion", ADAPTER_VERSION);
    cJSON_AddStringToObject(c, "protocol", STANDARD);
    cJSON_AddStringToObject(c, "standard", STANDARD);
    cJSON_AddStringToObject(c, "transport", "tcp-stream");
    cJSON_AddItemToObject(c, "linkModes", cJSON_CreateStringArray(modes, 2));
    cJSON* up = cJSON_AddObjectToObject(c, "upstream");
    cJSON_AddStringToObject(up, "name", "lib60870-C");
    cJSON_AddStringToObject(up, "version", UPSTREAM_VERSION);
    cJSON_AddStringToObject(up, "revision", UPSTREAM_REVISION);
    cJSON_AddStringToObject(up, "license", "GPL-3.0");
    cJSON_AddStringToObject(up, "language", "C");
    cJSON_AddTrueToObject(up, "independentEngine");

    cJSON* roles = cJSON_AddObjectToObject(c, "roles");
    cJSON_AddTrueToObject(roles, "server");
    cJSON_AddTrueToObject(roles, "client");

    cJSON* f = cJSON_AddObjectToObject(c, "features");
    static const char* on[] = { "generalInterrogation", "groupInterrogation", "counterInterrogation", "read",
        "clockSync", "testCommand", "directCommands", "selectBeforeOperate", "commandDeactivation",
        "spontaneousOnCommand", "pointQualityOnBitstring", "unbalancedMode", "balancedMode",
        "distinctBalancedAddresses" };
    static const char* off[] = { "timeTaggedCommands", "apciParameters", "multipleConnections", "dataTransferEvents",
        "tls", "tlsServer", "tlsClient", "fileTransfer", "fileServer", "fileClient", "fileUploadServer",
        "fileUploadClient", "fileDirectoryServer", "fileDirectoryClient", "redundancyServer", "redundancyClient",
        "secureAuthentication" };
    for (size_t i = 0; i < sizeof(on) / sizeof(on[0]); i++)
        cJSON_AddTrueToObject(f, on[i]);
    for (size_t i = 0; i < sizeof(off) / sizeof(off[0]); i++)
        cJSON_AddFalseToObject(f, off[i]);

    cJSON_AddItemToObject(c, "pointTypes", cJSON_CreateStringArray(pointTypes, 8));
    cJSON_AddItemToObject(c, "commandTypes", cJSON_CreateStringArray(commandTypes, 6));
    cJSON_AddItemToObject(c, "clientOperations", cJSON_CreateStringArray(operations, (int)(sizeof(operations) / sizeof(operations[0]))));
    emitJson(c);
    return EXIT_OK;
}

static int printFixture(const char* name)
{
    char path[256];
    if (strchr(name, '/') != NULL || strstr(name, "..") != NULL) {
        logf_("print-fixture: '%s' is not a fixture name", name);
        return EXIT_USAGE;
    }
    snprintf(path, sizeof(path), "/fixtures/%s/fixture.json", name);
    FILE* f = fopen(path, "rb");
    if (f == NULL) {
        logf_("print-fixture: no fixture named '%s' in this image", name);
        return EXIT_OPERATION_FAILED;
    }
    char buf[4096];
    size_t n;
    while ((n = fread(buf, 1, sizeof(buf), f)) > 0)
        fwrite(buf, 1, n, stdout);
    fclose(f);
    fflush(stdout);
    return EXIT_OK;
}

int main(int argc, char** argv)
{
    if (argc < 2) {
        fputs(USAGE, stderr);
        return EXIT_USAGE;
    }
    const char* cmd = argv[1];
    if (!strcmp(cmd, "server")) {
        Args a = { argc - 2, argv + 2 };
        return runServer(&a);
    }
    if (!strcmp(cmd, "client")) {
        if (argc < 3) {
            fputs(USAGE, stderr);
            return EXIT_USAGE;
        }
        Args a = { argc - 3, argv + 3 };
        return runClient(argv[2], &a);
    }
    if (!strcmp(cmd, "print-capabilities"))
        return printCapabilities();
    if (!strcmp(cmd, "print-fixture"))
        return printFixture(argc > 2 ? argv[2] : "baseline");
    if (!strcmp(cmd, "version")) {
        printf("%s %s (lib60870-C %s, IEC 60870-5-101)\n", ADAPTER_NAME, ADAPTER_VERSION, UPSTREAM_VERSION);
        return EXIT_OK;
    }
    if (!strcmp(cmd, "help") || !strcmp(cmd, "--help") || !strcmp(cmd, "-h")) {
        fputs(USAGE, stdout);
        return EXIT_OK;
    }
    fputs(USAGE, stderr);
    return EXIT_USAGE;
}
