// SPDX-License-Identifier: EPL-2.0
package org.otfabric.iec101interop;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.channel.ChannelDuplexHandler;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import java.io.ByteArrayOutputStream;

/**
 * Watches the FT 1.2 frames of the client's TCP connection without changing them.
 *
 * <p>digitalpetri's unbalanced master reports no event when a station's link
 * comes up (its start of data transfer completes once polling is armed), so the
 * adapter learns that the reset of the remote link was confirmed from the
 * frames themselves: a reset (primary, function 0) sent, then a positive
 * acknowledgement (secondary function 0, or the single character E5h)
 * received. With IEC101_TRACE=1 every frame is written to stderr.
 *
 * <p>On the server it also records which ASDUs have left ({@link Sent}).
 */
final class LinkTap extends ChannelDuplexHandler {
    private static final boolean TRACE = "1".equals(System.getenv("IEC101_TRACE"));

    private final int addrSize;
    private final ByteArrayOutputStream inbound = new ByteArrayOutputStream();
    private boolean resetSent;
    private boolean linkUp;
    private final Runnable onLinkUp;
    private final Sent sent;

    LinkTap(int addrSize, Runnable onLinkUp) {
        this(addrSize, onLinkUp, null);
    }

    LinkTap(int addrSize, Runnable onLinkUp, Sent sent) {
        this.addrSize = addrSize;
        this.onLinkUp = onLinkUp;
        this.sent = sent;
    }

    /**
     * The ASDUs written to the line, by type identification and cause.
     *
     * <p>digitalpetri's unbalanced secondary queues a spontaneous ASDU in class 1
     * and every other cause in class 2, and serves the polls from its own
     * thread. Order holds within a class only, so the station waits until an
     * ASDU has left before it queues the next one where the order matters
     * (confirmation, report, termination of a command).
     */
    static final class Sent {
        private static final int KEEP = 64;
        private final long[] seqs = new long[KEEP];
        private final int[] types = new int[KEEP];
        private final int[] causes = new int[KEEP];
        private long seq;

        synchronized long mark() {
            return seq;
        }

        synchronized void record(int type, int cause) {
            int i = (int) (seq % KEEP);
            seq++;
            seqs[i] = seq;
            types[i] = type;
            causes[i] = cause;
            notifyAll();
        }

        /** Waits until an ASDU of the type and cause was written after mark. */
        synchronized boolean await(long mark, int type, int cause, long timeoutMillis) {
            long deadline = System.nanoTime() + timeoutMillis * 1_000_000L;
            while (true) {
                for (int i = 0; i < KEEP; i++) {
                    if (seqs[i] > mark && types[i] == type && causes[i] == cause) {
                        return true;
                    }
                }
                long left = (deadline - System.nanoTime()) / 1_000_000L;
                if (left <= 0) {
                    return false;
                }
                try {
                    wait(left);
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                    return false;
                }
            }
        }
    }

    @Override
    public void channelRead(ChannelHandlerContext ctx, Object msg) throws Exception {
        if (msg instanceof ByteBuf b) {
            byte[] chunk = ByteBufUtil.getBytes(b);
            synchronized (this) {
                inbound.write(chunk, 0, chunk.length);
                drain();
            }
        }
        super.channelRead(ctx, msg);
    }

    @Override
    public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) throws Exception {
        if (msg instanceof ByteBuf b) {
            byte[] frame = ByteBufUtil.getBytes(b);
            if (TRACE) {
                System.err.println("[digitalpetri] -> " + ByteBufUtil.hexDump(frame));
            }
            synchronized (this) {
                Integer control = control(frame);
                if (control != null && (control & 0x40) != 0) { // PRM: a primary frame
                    resetSent = (control & 0x0f) == 0;
                }
            }
            int at = 5 + addrSize; // the ASDU of a frame with variable length
            if (sent != null && frame.length > at + 2 && (frame[0] & 0xff) == 0x68) {
                sent.record(frame[at] & 0xff, frame[at + 2] & 0x3f);
            }
        }
        super.write(ctx, msg, promise);
    }

    /** The control field of a whole frame, or null for the single character. */
    private static Integer control(byte[] f) {
        if (f.length >= 2 && (f[0] & 0xff) == 0x10) {
            return f[1] & 0xff;
        }
        if (f.length >= 5 && (f[0] & 0xff) == 0x68) {
            return f[4] & 0xff;
        }
        return null;
    }

    /** Splits the received octets into frames. Called with the lock held. */
    private void drain() {
        byte[] buf = inbound.toByteArray();
        int at = 0;
        while (at < buf.length) {
            int start = buf[at] & 0xff;
            int len;
            if (start == 0xE5) {
                len = 1;
            } else if (start == 0x10) {
                len = 4 + addrSize;
            } else if (start == 0x68) {
                if (buf.length - at < 2) {
                    break;
                }
                len = (buf[at + 1] & 0xff) + 6;
            } else {
                at++; // not the start of a frame: skip it, as a receiver would
                continue;
            }
            if (buf.length - at < len) {
                break;
            }
            byte[] frame = new byte[len];
            System.arraycopy(buf, at, frame, 0, len);
            at += len;
            received(frame);
        }
        inbound.reset();
        inbound.write(buf, at, buf.length - at);
    }

    private void received(byte[] frame) {
        if (TRACE) {
            System.err.println("[digitalpetri] <- " + ByteBufUtil.hexDump(frame));
        }
        Integer control = control(frame);
        boolean ack = frame.length == 1 || control != null && (control & 0x40) == 0 && (control & 0x0f) == 0;
        if (resetSent && ack && !linkUp) {
            linkUp = true;
            onLinkUp.run();
        }
        if (control != null && (control & 0x40) == 0) {
            resetSent = false; // the answer to whatever was outstanding
        } else if (frame.length == 1) {
            resetSent = false;
        }
    }
}
