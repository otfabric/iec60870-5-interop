/* SPDX-License-Identifier: GPL-3.0-or-later */
#include "common.h"

#include <string.h>

bool linkFromArgs(Args* a, LinkArgs* out)
{
    memset(out, 0, sizeof(*out));
    const char* mode = argString(a, "--link-mode", "unbalanced");
    if (!strcmp(mode, "balanced"))
        out->balanced = true;
    else if (strcmp(mode, "unbalanced") != 0) {
        logf_("--link-mode: must be unbalanced or balanced, got '%s'", mode);
        return false;
    }
    int remote = -1;
    if (!argInt(a, "--link-address-size", 1, &out->addrSize) || !argInt(a, "--link-address", 1, &out->address) ||
        !argInt(a, "--remote-link-address", -1, &remote) || !argInt(a, "--cot-size", 1, &out->cotSize) ||
        !argInt(a, "--ca-size", 1, &out->caSize) || !argInt(a, "--ioa-size", 2, &out->ioaSize) ||
        !argInt(a, "--common-address", 0, &out->commonAddress))
        return false;
    if (out->addrSize < 0 || out->addrSize > 2 || (out->addrSize == 0 && !out->balanced)) {
        logf_("--link-address-size: must be 1 or 2 (0 only in balanced mode)");
        return false;
    }
    if (out->cotSize < 1 || out->cotSize > 2 || out->caSize < 1 || out->caSize > 2 || out->ioaSize < 1 ||
        out->ioaSize > 3) {
        logf_("--cot-size and --ca-size must be 1 or 2, --ioa-size 1, 2 or 3");
        return false;
    }
    /* All ones is the broadcast address. */
    int maxAddr = out->addrSize == 0 ? 0 : (1 << (8 * out->addrSize)) - 2;
    if (out->address < 0 || out->address > maxAddr) {
        logf_("--link-address: must be 0..%d for a link address of %d octets", maxAddr, out->addrSize);
        return false;
    }
    if (remote >= 0 && !out->balanced) {
        logf_("--remote-link-address is for balanced mode only");
        return false;
    }
    out->remote = remote >= 0 ? remote : out->address;
    if (out->remote > maxAddr) {
        logf_("--remote-link-address: must be 0..%d", maxAddr);
        return false;
    }
    int maxCA = (1 << (8 * out->caSize)) - 1;
    if (out->commonAddress < 0 || out->commonAddress > maxCA) {
        logf_("--common-address: must be 1..%d for a common address of %d octets", maxCA, out->caSize);
        return false;
    }
    return true;
}

void applyLinkArgs(const LinkArgs* l, LinkLayerParameters ll, CS101_AppLayerParameters al)
{
    ll->addressLength = l->addrSize;
    /* lib60870's defaults are for a serial line: an ACK within 200 ms, a
     * request repeated for 1 s. On the TCP stream answers take a few ms. */
    ll->timeoutForAck = 500;
    ll->timeoutRepeat = 2000;
    ll->useSingleCharACK = true;
    ll->timeoutLinkState = 500;
    al->sizeOfTypeId = 1;
    al->sizeOfVSQ = 1;
    al->sizeOfCOT = l->cotSize;
    al->originatorAddress = 0;
    al->sizeOfCA = l->caSize;
    al->sizeOfIOA = l->ioaSize;
    al->maxSizeOfASDU = 249;
}
