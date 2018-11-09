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
import static com.google.common.base.Charsets.UTF_8;
import static io.grpc.Metadata.ASCII_STRING_MARSHALLER;
import static io.grpc.Metadata.BINARY_BYTE_MARSHALLER;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.collect.Iterables;
import com.google.common.io.BaseEncoding;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.Metadata.BinaryMarshaller;
import io.grpc.Metadata.Key;
import java.util.Arrays;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link TransportFrameUtil}. */
@RunWith(JUnit4.class)
public class TransportFrameUtilTest {

  private static final String NONCOMPLIANT_ASCII_STRING = new String(new char[]{1, 2, 3});

  private static final String COMPLIANT_ASCII_STRING = "Kyle";

  private static final BinaryMarshaller<String> UTF8_STRING_MARSHALLER =
      new BinaryMarshaller<String>() {
    @Override
    public byte[] toBytes(String value) {
      return value.getBytes(UTF_8);
    }

    @Override
    public String parseBytes(byte[] serialized) {
      return new String(serialized, UTF_8);
    }
  };

  private static final Key<String> PLAIN_STRING = Key.of("plainstring", ASCII_STRING_MARSHALLER);
  private static final Key<String> BINARY_STRING = Key.of("string-bin", UTF8_STRING_MARSHALLER);
  private static final Key<String> BINARY_STRING_WITHOUT_SUFFIX =
      Key.of("string", ASCII_STRING_MARSHALLER);
  private static final Key<byte[]> BINARY_BYTES = Key.of("bytes-bin", BINARY_BYTE_MARSHALLER);

  @Test
  public void testToHttp2Headers() {
    Metadata headers = new Metadata();
    headers.put(PLAIN_STRING, COMPLIANT_ASCII_STRING);
    headers.put(BINARY_STRING, NONCOMPLIANT_ASCII_STRING);
    headers.put(BINARY_STRING_WITHOUT_SUFFIX, NONCOMPLIANT_ASCII_STRING);
    byte[][] http2Headers = TransportFrameUtil.toHttp2Headers(headers);
    // BINARY_STRING_WITHOUT_SUFFIX should not get in because it contains non-compliant ASCII
    // characters but doesn't have "-bin" in the name.
    byte[][] answer = new byte[][] {
        "plainstring".getBytes(US_ASCII), COMPLIANT_ASCII_STRING.getBytes(US_ASCII),
        "string-bin".getBytes(US_ASCII),
        base64Encode(NONCOMPLIANT_ASCII_STRING.getBytes(US_ASCII))};
    assertEquals(answer.length, http2Headers.length);
    // http2Headers may re-sort the keys, so we cannot compare it with the answer side-by-side.
    for (int i = 0; i < answer.length; i += 2) {
      assertContains(http2Headers, answer[i], answer[i + 1]);
    }
  }

  @Test(expected = IllegalArgumentException.class)
  public void binaryHeaderWithoutSuffix() {
    Key.of("plainstring", UTF8_STRING_MARSHALLER);
  }

  @Test
  public void testToAndFromHttp2Headers() {
    Metadata headers = new Metadata();
    headers.put(PLAIN_STRING, COMPLIANT_ASCII_STRING);
    headers.put(BINARY_STRING, NONCOMPLIANT_ASCII_STRING);
    headers.put(BINARY_STRING_WITHOUT_SUFFIX, NONCOMPLIANT_ASCII_STRING);
    byte[][] http2Headers = TransportFrameUtil.toHttp2Headers(headers);
    byte[][] rawSerialized = TransportFrameUtil.toRawSerializedHeaders(http2Headers);
    Metadata recoveredHeaders = InternalMetadata.newMetadata(rawSerialized);
    assertEquals(COMPLIANT_ASCII_STRING, recoveredHeaders.get(PLAIN_STRING));
    assertEquals(NONCOMPLIANT_ASCII_STRING, recoveredHeaders.get(BINARY_STRING));
    assertNull(recoveredHeaders.get(BINARY_STRING_WITHOUT_SUFFIX));
  }

  @Test
  public void dupBinHeadersWithComma() {
    byte[][] http2Headers = new byte[][] {
        BINARY_BYTES.name().getBytes(US_ASCII),
        "BaS,e6,,4+,padding==".getBytes(US_ASCII),
        BINARY_BYTES.name().getBytes(US_ASCII),
        "more".getBytes(US_ASCII),
        BINARY_BYTES.name().getBytes(US_ASCII),
        "".getBytes(US_ASCII)};
    byte[][] rawSerialized = TransportFrameUtil.toRawSerializedHeaders(http2Headers);
    Metadata recoveredHeaders = InternalMetadata.newMetadata(rawSerialized);
    byte[][] values = Iterables.toArray(recoveredHeaders.getAll(BINARY_BYTES), byte[].class);

    assertTrue(Arrays.deepEquals(
        new byte[][] {
            BaseEncoding.base64().decode("BaS"),
            BaseEncoding.base64().decode("e6"),
            BaseEncoding.base64().decode(""),
            BaseEncoding.base64().decode("4+"),
            BaseEncoding.base64().decode("padding"),
            BaseEncoding.base64().decode("more"),
            BaseEncoding.base64().decode("")},
        values));
  }

  private static void assertContains(byte[][] headers, byte[] key, byte[] value) {
    String keyString = new String(key, US_ASCII);
    for (int i = 0; i < headers.length; i += 2) {
      if (Arrays.equals(headers[i], key)) {
        assertArrayEquals("value for key=" + keyString, value, headers[i + 1]);
        return;
      }
    }
    fail("key=" + keyString + " not found");
  }

  private static byte[] base64Encode(byte[] input) {
    return InternalMetadata.BASE64_ENCODING_OMIT_PADDING.encode(input).getBytes(US_ASCII);
  }

}
