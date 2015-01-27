/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc;

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
  public static final byte STATUS_FRAME = 0x3;
  public static final byte FRAME_TYPE_MASK = 0x3;

  /**
   * No. of bytes for length field within a frame
   */
  public static final int FRAME_LENGTH = 4;

  public static boolean isPayloadFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == PAYLOAD_FRAME;
  }

  public static boolean isStatusFrame(byte flags) {
    return (flags & FRAME_TYPE_MASK) == STATUS_FRAME;
  }
}
