package com.google.net.stubby;

/**
 * Common constants and utilities for GRPC protocol framing.
 * The format within the data stream provided by the transport layer is simply
 *
 * stream         = frame+
 * frame          = frame-type framed-message
 * frame-type     = payload-type | context-type | status-type
 * framed-message = payload | context | status
 * payload        = length <bytes>
 * length         = <uint32>
 * context        = context-key context-value
 * context-key    = length str
 * context-value  = length <bytes>
 * status         = TBD
 *
 * frame-type is implemented as a bitmask within a single byte
 *
 */
public class GrpcFramingUtil {
  /**
   * Length of flags block in bytes
   */
  public static final int FRAME_TYPE_LENGTH = 1;

  // Flags
  public static final byte PAYLOAD_FRAME = 0x0;
  public static final byte CONTEXT_VALUE_FRAME =  0x1;
  public static final byte STATUS_FRAME = 0x2;
  public static final byte RESERVED_FRAME = 0x3;
  public static final byte FRAME_TYPE_MASK = 0x3;

  /**
   * No. of bytes for length field within a frame
   */
  public static final int FRAME_LENGTH = 4;

  public static boolean isContextValueFrame(int flags) {
    return (flags & FRAME_TYPE_MASK) == CONTEXT_VALUE_FRAME;
  }

  public static boolean isPayloadFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == PAYLOAD_FRAME;
  }

  public static boolean isStatusFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == STATUS_FRAME;
  }
}
