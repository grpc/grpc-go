package com.google.net.stubby;

/**
 * Common constants for protocol framing. The format within the data stream is
 *
 * | Flags (1 byte) | flag-specific message |
 *
 * the flags block has the form
 *
 * | Reserved (5) | Compressed (1) | Frame Type (2) |
 */
public class ProtocolConstants {
  /**
   * Length of flags block
   */
  public static final int FLAGS_LENGTH = 1;

  // Flags
  public static final int PAYLOAD_FRAME = 0x0;
  public static final int CONTEXT_VALUE_FRAME =  0x1;
  public static final int RESPONSE_STATUS_FRAME = 0x2;
  public static final int RESERVED_FRAME = 0x3;
  public static final int FRAME_TYPE_MASK = 0x3;
  public static final int COMPRESSED_FLAG = 0x4;

  /**
   * No. of bytes for the length of each data stream frame
   */
  public static final int FRAME_LENGTH = 4;

  public static boolean isContextValueFrame(int flags) {
    return (flags & FRAME_TYPE_MASK)  == CONTEXT_VALUE_FRAME;
  }

  public static boolean isPayloadFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == PAYLOAD_FRAME;
  }

  public static boolean isCompressed(int flags) {
    return (flags & COMPRESSED_FLAG) != 0;
  }
}
