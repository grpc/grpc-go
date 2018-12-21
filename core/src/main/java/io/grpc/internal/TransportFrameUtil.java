/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.internal;

import static com.google.common.base.Charsets.US_ASCII;

import com.google.common.io.BaseEncoding;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.logging.Logger;
import javax.annotation.CheckReturnValue;

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

  /**
   * Transform the given headers to a format where only spec-compliant ASCII characters are allowed.
   * Binary header values are encoded by Base64 in the result.  It is safe to modify the returned
   * array, but not to modify any of the underlying byte arrays.
   *
   * @return the interleaved keys and values.
   */
  public static byte[][] toHttp2Headers(Metadata headers) {
    byte[][] serializedHeaders = InternalMetadata.serialize(headers);
    // TODO(carl-mastrangelo): eventually remove this once all callers are updated.
    if (serializedHeaders == null) {
      return new byte[][]{};
    }
    int k = 0;
    for (int i = 0; i < serializedHeaders.length; i += 2) {
      byte[] key = serializedHeaders[i];
      byte[] value = serializedHeaders[i + 1];
      if (endsWith(key, binaryHeaderSuffixBytes)) {
        // Binary header.
        serializedHeaders[k] = key;
        serializedHeaders[k + 1]
            = InternalMetadata.BASE64_ENCODING_OMIT_PADDING.encode(value).getBytes(US_ASCII);
        k += 2;
      } else {
        // Non-binary header.
        // Filter out headers that contain non-spec-compliant ASCII characters.
        // TODO(zhangkun83): only do such check in development mode since it's expensive
        if (isSpecCompliantAscii(value)) {
          serializedHeaders[k] = key;
          serializedHeaders[k + 1] = value;
          k += 2;
        } else {
          String keyString = new String(key, US_ASCII);
          logger.warning("Metadata key=" + keyString + ", value=" + Arrays.toString(value)
              + " contains invalid ASCII characters");
        }
      }
    }
    // Fast path, everything worked out fine.
    if (k == serializedHeaders.length) {
      return serializedHeaders;
    }
    return Arrays.copyOfRange(serializedHeaders, 0, k);
  }

  /**
   * Transform HTTP/2-compliant headers to the raw serialized format which can be deserialized by
   * metadata marshallers. It decodes the Base64-encoded binary headers.
   *
   * <p>Warning: This function may partially modify the headers in place by modifying the input
   * array (but not modifying any single byte), so the input reference {@code http2Headers} can not
   * be used again.
   *
   * @param http2Headers the interleaved keys and values of HTTP/2-compliant headers
   * @return the interleaved keys and values in the raw serialized format
   */
  @CheckReturnValue
  public static byte[][] toRawSerializedHeaders(byte[][] http2Headers) {
    for (int i = 0; i < http2Headers.length; i += 2) {
      byte[] key = http2Headers[i];
      byte[] value = http2Headers[i + 1];
      if (endsWith(key, binaryHeaderSuffixBytes)) {
        // Binary header
        for (int idx = 0; idx < value.length; idx++) {
          if (value[idx] == (byte) ',') {
            return serializeHeadersWithCommasInBin(http2Headers, i);
          }
        }
        byte[] decodedVal = BaseEncoding.base64().decode(new String(value, US_ASCII));
        http2Headers[i + 1] = decodedVal;
      } else {
        // Non-binary header
        // Nothing to do, the value is already in the right place.
      }
    }
    return http2Headers;
  }

  private static byte[][] serializeHeadersWithCommasInBin(byte[][] http2Headers, int resumeFrom) {
    List<byte[]> headerList = new ArrayList<>(http2Headers.length + 10);
    for (int i = 0; i < resumeFrom; i++) {
      headerList.add(http2Headers[i]);
    }
    for (int i = resumeFrom; i < http2Headers.length; i += 2) {
      byte[] key = http2Headers[i];
      byte[] value = http2Headers[i + 1];
      if (!endsWith(key, binaryHeaderSuffixBytes)) {
        headerList.add(key);
        headerList.add(value);
        continue;
      }
      // Binary header
      int prevIdx = 0;
      for (int idx = 0; idx <= value.length; idx++) {
        if (idx != value.length && value[idx] != (byte) ',') {
          continue;
        }
        byte[] decodedVal =
            BaseEncoding.base64().decode(new String(value, prevIdx, idx - prevIdx, US_ASCII));
        prevIdx = idx + 1;
        headerList.add(key);
        headerList.add(decodedVal);
      }
    }
    return headerList.toArray(new byte[0][]);
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
