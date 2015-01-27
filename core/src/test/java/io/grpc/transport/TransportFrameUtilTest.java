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
import static com.google.common.base.Charsets.UTF_8;
import static io.grpc.Metadata.ASCII_STRING_MARSHALLER;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.fail;

import com.google.common.io.BaseEncoding;

import io.grpc.Metadata.BinaryMarshaller;
import io.grpc.Metadata.Headers;
import io.grpc.Metadata.Key;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.Arrays;

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

  @Test
  public void testToHttp2Headers() {
    Headers headers = new Headers();
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
    Headers headers = new Headers();
    headers.put(PLAIN_STRING, COMPLIANT_ASCII_STRING);
    headers.put(BINARY_STRING, NONCOMPLIANT_ASCII_STRING);
    headers.put(BINARY_STRING_WITHOUT_SUFFIX, NONCOMPLIANT_ASCII_STRING);
    byte[][] http2Headers = TransportFrameUtil.toHttp2Headers(headers);
    byte[][] rawSerialized = TransportFrameUtil.toRawSerializedHeaders(http2Headers);
    Headers recoveredHeaders = new Headers(rawSerialized);
    assertEquals(COMPLIANT_ASCII_STRING, recoveredHeaders.get(PLAIN_STRING));
    assertEquals(NONCOMPLIANT_ASCII_STRING, recoveredHeaders.get(BINARY_STRING));
    assertNull(recoveredHeaders.get(BINARY_STRING_WITHOUT_SUFFIX));
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
    return BaseEncoding.base64().encode(input).getBytes(US_ASCII);
  }

}
