// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import com.digitalpetri.iec60870.ProtocolProfile;
import com.digitalpetri.iec60870.cs101.LinkSettings;
import java.time.Duration;
import java.util.List;

/** The IEC 101 link and the field sizes of the contract's link flags. */
final class LinkConfig {
    boolean balanced;
    int address; // the controlled station's link address
    int remote; // balanced: the controlling station's link address
    int addrSize;
    int cotSize;
    int caSize;
    int ioaSize;
    int commonAddr; // --common-address, 0 when not given

    static LinkConfig parse(Args a) {
        LinkConfig c = new LinkConfig();
        String mode = a.string("--link-mode", "unbalanced");
        switch (mode) {
            case "unbalanced" -> c.balanced = false;
            case "balanced" -> c.balanced = true;
            default -> throw Args.usage("--link-mode: must be unbalanced or balanced, got '%s'", mode);
        }
        c.addrSize = a.integer("--link-address-size", 1);
        c.address = a.integer("--link-address", 1);
        String remote = a.string("--remote-link-address", null);
        c.cotSize = a.integer("--cot-size", 1);
        c.caSize = a.integer("--ca-size", 1);
        c.ioaSize = a.integer("--ioa-size", 2);
        c.commonAddr = a.integer("--common-address", 0);
        if (c.addrSize < 0 || c.addrSize > 2) {
            throw Args.usage("--link-address-size: must be 0, 1 or 2");
        }
        if (c.addrSize == 0 && !c.balanced) {
            throw Args.usage("--link-address-size 0 is for balanced mode only");
        }
        if (c.cotSize < 1 || c.cotSize > 2) {
            throw Args.usage("--cot-size: must be 1 or 2");
        }
        if (c.caSize < 1 || c.caSize > 2) {
            throw Args.usage("--ca-size: must be 1 or 2");
        }
        if (c.ioaSize < 1 || c.ioaSize > 3) {
            throw Args.usage("--ioa-size: must be 1, 2 or 3");
        }
        int maxAddr = c.addrSize == 0 ? 0 : (1 << (8 * c.addrSize)) - 2; // all ones is the broadcast address
        if (c.address < 0 || c.address > maxAddr) {
            throw Args.usage("--link-address: must be 0..%d for a link address of %d octets", maxAddr, c.addrSize);
        }
        c.remote = c.address;
        if (remote != null) {
            if (!c.balanced) {
                throw Args.usage("--remote-link-address is for balanced mode only");
            }
            try {
                c.remote = Integer.parseInt(remote);
            } catch (NumberFormatException e) {
                throw Args.usage("--remote-link-address: '%s' is not an integer", remote);
            }
            if (c.remote < 0 || c.remote > maxAddr) {
                throw Args.usage("--remote-link-address: must be 0..%d", maxAddr);
            }
        }
        // digitalpetri's balanced link puts the station's own address in every
        // frame it sends and does not look at the address of what it receives;
        // two addresses on one line is something this adapter does not claim.
        if (c.balanced && c.remote != c.address) {
            throw Args.usage("--remote-link-address: this adapter uses one link address in both directions; "
                    + "it must equal --link-address");
        }
        int maxCA = (1 << (8 * c.caSize)) - 1;
        if (c.commonAddr < 0 || c.commonAddr > maxCA) {
            throw Args.usage("--common-address: must be 1..%d for a common address of %d octets", maxCA, c.caSize);
        }
        return c;
    }

    int broadcastCA() {
        return (1 << (8 * caSize)) - 1;
    }

    String modeName() {
        return balanced ? "balanced" : "unbalanced";
    }

    ProtocolProfile profile() {
        return new ProtocolProfile(cotSize, caSize, ioaSize, 255);
    }

    /** Link settings of the controlling station (client) or of the controlled station. */
    LinkSettings settings(boolean client) {
        int broadcast = addrSize == 0 ? 255 : (1 << (8 * addrSize)) - 1;
        LinkSettings.Builder b = balanced ? LinkSettings.balanced() : LinkSettings.unbalanced();
        // The answer timeout of a request: the interval of IEC 60870-5-2 is far
        // shorter on a TCP stream than on a slow line; the retries cover a slow peer.
        b.linkAddress(balanced && !client ? remote : address)
                .linkAddressLength(addrSize)
                .confirmTimeout(Duration.ofMillis(500))
                .repeatTimeout(Duration.ofMillis(500))
                .maxRetries(3);
        if (!balanced) {
            b.broadcastAddress(broadcast);
            if (client) {
                b.slaveAddresses(List.of(address)).pollInterval(Duration.ofMillis(50));
            }
        }
        return b.build();
    }
}
