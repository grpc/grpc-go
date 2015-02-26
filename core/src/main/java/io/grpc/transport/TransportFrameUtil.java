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

package io.grpc.transport;

import static com.google.common.base.Charsets.US_ASCII;

import com.google.common.io.BaseEncoding;

import io.grpc.Metadata;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.logging.Logger;

import javax.annotation.Nullable;

/**
 * Utility functions for transport layer framing.
 *
 * <p>Within a given transport frame we reserve the first byte to indicate the type of compression
 * used for the contents of the transport frame.
 */
public final class TransportFrameUtil {

  private static final Logger logger = Logger.getLogger(TransportFrameUtil.class.getName());

  private static final byte[] binaryHeaderSuffixBytes =
      Metadata.BINARY_HEADER_SUFFIX.getBytes(US_ASCII);

  // TODO(louiscryan): This needs proper namespacing support, this is currently just a hack
  /**
   * Converts the path from the HTTP request to the fully qualified method name.
   *
   * @return the fully qualified method name. {@code null} if the path is malformatted.
   */
  @Nullable
  public static String getFullMethodNameFromPath(String path) {
    if (!path.startsWith("/")) {
      return null;
    }
    return path;
  }

  /**
   * Transform the given headers to a format where only spec-compliant ASCII characters are allowed.
   * Binary header values are encoded by Base64 in the result.
   *
   * @return the interleaved keys and values.
   */
  public static byte[][] toHttp2Headers(Metadata headers) {
    byte[][] serializedHeaders = headers.serialize();
    ArrayList<byte[]> result = new ArrayList<byte[]>();
    for (int i = 0; i < serializedHeaders.length; i += 2) {
      byte[] key = serializedHeaders[i];
      byte[] value = serializedHeaders[i + 1];
      if (endsWith(key, binaryHeaderSuffixBytes)) {
        // Binary header.
        result.add(key);
        result.add(BaseEncoding.base64().encode(value).getBytes(US_ASCII));
      } else {
        // Non-binary header.
        // Filter out headers that contain non-spec-compliant ASCII characters.
        // TODO(zhangkun83): only do such check in development mode since it's expensive
        if (isSpecCompliantAscii(value)) {
          result.add(key);
          result.add(value);
        } else {
          String keyString = new String(key, US_ASCII);
          logger.warning("Metadata key=" + keyString + ", value=" + Arrays.toString(value)
              + " contains invalid ASCII characters");
        }
      }
    }
    return result.toArray(new byte[result.size()][]);
  }

  /**
   * Transform HTTP/2-compliant headers to the raw serialized format which can be deserialized by
   * metadata marshallers. It decodes the Base64-encoded binary headers.
   *
   * @param http2Headers the interleaved keys and values of HTTP/2-compliant headers
   * @return the interleaved keys and values in the raw serialized format
   */
  public static byte[][] toRawSerializedHeaders(byte[][] http2Headers) {
    byte[][] result = new byte[http2Headers.length][];
    for (int i = 0; i < http2Headers.length; i += 2) {
      byte[] key = http2Headers[i];
      byte[] value = http2Headers[i + 1];
      result[i] = key;
      if (endsWith(key, binaryHeaderSuffixBytes)) {
        // Binary header
        result[i + 1] = BaseEncoding.base64().decode(new String(value, US_ASCII));
      } else {
        // Non-binary header
        result[i + 1] = value;
      }
    }
    return result;
  }

  /**
   * Returns {@code true} if {@code subject} ends with {@code suffix}.
   */
  private static boolean endsWith(byte[] subject, byte[] suffix) {
    int start = subject.length - suffix.length;
    if (start < 0) {
      return false;
    }
    for (int i = start; i < subject.length; i++) {
      if (subject[i] != suffix[i - start]) {
        return false;
      }
    }
    return true;
  }

  /**
   * Returns {@code true} if {@code subject} contains only bytes that are spec-compliant ASCII
   * characters and space.
   */
  private static boolean isSpecCompliantAscii(byte[] subject) {
    for (byte b : subject) {
      if (b < 32 || b > 126) {
        return false;
      }
    }
    return true;
  }

  private TransportFrameUtil() {}
}
