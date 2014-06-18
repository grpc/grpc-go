package com.google.net.stubby.newtransport;

/**
 * Utility functions for transport layer framing.
 *
 * Within a given transport frame we reserve the first byte to indicate the
 * type of compression used for the contents of the transport frame.
 */
public class TransportFrameUtil {

  // Compression modes (lowest order 3 bits of frame flags)
  public static final byte NO_COMPRESS_FLAG = 0x0;
  public static final byte FLATE_FLAG =  0x1;
  public static final byte COMPRESSION_FLAG_MASK = 0x7;

  public static boolean isNotCompressed(int b) {
    return ((b & COMPRESSION_FLAG_MASK) == NO_COMPRESS_FLAG);
  }

  public static boolean isFlateCompressed(int b) {
    return ((b & COMPRESSION_FLAG_MASK) == FLATE_FLAG);
  }
}
