package com.google.net.stubby.newtransport;

import javax.annotation.Nullable;

/**
 * Utility functions for transport layer framing.
 *
 * <p>Within a given transport frame we reserve the first byte to indicate the type of compression
 * used for the contents of the transport frame.
 */
public final class TransportFrameUtil {

  // Compression modes (lowest order 3 bits of frame flags)
  public static final byte NO_COMPRESS_FLAG = 0x0;
  public static final byte FLATE_FLAG = 0x1;
  public static final byte COMPRESSION_FLAG_MASK = 0x7;

  public static boolean isNotCompressed(int b) {
    return ((b & COMPRESSION_FLAG_MASK) == NO_COMPRESS_FLAG);
  }

  public static boolean isFlateCompressed(int b) {
    return ((b & COMPRESSION_FLAG_MASK) == FLATE_FLAG);
  }

  /**
   * Length of the compression type field.
   */
  public static final int COMPRESSION_TYPE_LENGTH = 1;

  /**
   * Length of the compression frame length field.
   */
  public static final int COMPRESSION_FRAME_LENGTH = 3;

  /**
   * Full length of the compression header.
   */
  public static final int COMPRESSION_HEADER_LENGTH =
      COMPRESSION_TYPE_LENGTH + COMPRESSION_FRAME_LENGTH;

  /**
   * Length of flags block in bytes
   */
  public static final int FRAME_TYPE_LENGTH = 1;

  // Flags
  public static final byte PAYLOAD_FRAME = 0x0;
  public static final byte CONTEXT_VALUE_FRAME = 0x1;
  public static final byte CALL_HEADER_FRAME = 0x2;
  public static final byte STATUS_FRAME = 0x3;
  public static final byte FRAME_TYPE_MASK = 0x3;

  /**
   * Number of bytes for the length field within a frame
   */
  public static final int FRAME_LENGTH = 4;

  /**
   * Full length of the GRPC frame header.
   */
  public static final int FRAME_HEADER_LENGTH = FRAME_TYPE_LENGTH + FRAME_LENGTH;

  public static boolean isContextValueFrame(int flags) {
    return (flags & FRAME_TYPE_MASK) == CONTEXT_VALUE_FRAME;
  }

  public static boolean isPayloadFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == PAYLOAD_FRAME;
  }

  public static boolean isStatusFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == STATUS_FRAME;
  }

  // TODO(user): This needs proper namespacing support, this is currently just a hack
  /**
   * Converts the path from the HTTP request to the full qualified method name.
   *
   * @return null if the path is malformatted.
   */
  @Nullable
  public static String getFullMethodNameFromPath(String path) {
    if (!path.startsWith("/")) {
      return null;
    }
    return path.substring(1);
  }

  private TransportFrameUtil() {}
}
