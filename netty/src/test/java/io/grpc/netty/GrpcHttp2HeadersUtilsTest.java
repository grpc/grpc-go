/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.netty;

import static io.grpc.Metadata.BINARY_BYTE_MARSHALLER;
import static io.grpc.internal.GrpcUtil.DEFAULT_MAX_HEADER_LIST_SIZE;
import static io.netty.util.AsciiString.of;
import static org.hamcrest.CoreMatchers.containsString;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertThat;
import static org.junit.Assert.assertTrue;

import com.google.common.collect.Iterables;
import com.google.common.io.BaseEncoding;
import io.grpc.Metadata;
import io.grpc.Metadata.Key;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ClientHeadersDecoder;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2RequestHeaders;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ServerHeadersDecoder;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.DefaultHttp2HeadersEncoder;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2HeadersDecoder;
import io.netty.handler.codec.http2.Http2HeadersEncoder;
import io.netty.handler.codec.http2.Http2HeadersEncoder.SensitivityDetector;
import io.netty.util.AsciiString;
import java.util.Arrays;
import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link GrpcHttp2HeadersUtils}.
 */
@RunWith(JUnit4.class)
@SuppressWarnings({ "BadImport", "UndefinedEquals" }) // AsciiString.of and AsciiString.equals
public class GrpcHttp2HeadersUtilsTest {

  private static final SensitivityDetector NEVER_SENSITIVE = new SensitivityDetector() {
    @Override
    public boolean isSensitive(CharSequence name, CharSequence value) {
      return false;
    }
  };

  private ByteBuf encodedHeaders;

  @After
  public void tearDown() {
    if (encodedHeaders != null) {
      encodedHeaders.release();
    }
  }

  @Test
  public void decode_requestHeaders() throws Http2Exception {
    Http2HeadersDecoder decoder = new GrpcHttp2ServerHeadersDecoder(DEFAULT_MAX_HEADER_LIST_SIZE);
    Http2HeadersEncoder encoder =
        new DefaultHttp2HeadersEncoder(NEVER_SENSITIVE);

    Http2Headers headers = new DefaultHttp2Headers(false);
    headers.add(of(":scheme"), of("https")).add(of(":method"), of("GET"))
        .add(of(":path"), of("index.html")).add(of(":authority"), of("foo.grpc.io"))
        .add(of("custom"), of("header"));
    encodedHeaders = Unpooled.buffer();
    encoder.encodeHeaders(1 /* randomly chosen */, headers, encodedHeaders);

    Http2Headers decodedHeaders = decoder.decodeHeaders(3 /* randomly chosen */, encodedHeaders);
    assertEquals(headers.get(of(":scheme")), decodedHeaders.scheme());
    assertEquals(headers.get(of(":method")), decodedHeaders.method());
    assertEquals(headers.get(of(":path")), decodedHeaders.path());
    assertEquals(headers.get(of(":authority")), decodedHeaders.authority());
    assertEquals(headers.get(of("custom")), decodedHeaders.get(of("custom")));
    assertEquals(headers.size(), decodedHeaders.size());

    String toString = decodedHeaders.toString();
    assertContainsKeyAndValue(toString, ":scheme", decodedHeaders.scheme());
    assertContainsKeyAndValue(toString, ":method", decodedHeaders.method());
    assertContainsKeyAndValue(toString, ":path", decodedHeaders.path());
    assertContainsKeyAndValue(toString, ":authority", decodedHeaders.authority());
    assertContainsKeyAndValue(toString, "custom", decodedHeaders.get(of("custom")));
  }

  @Test
  public void decode_responseHeaders() throws Http2Exception {
    Http2HeadersDecoder decoder = new GrpcHttp2ClientHeadersDecoder(DEFAULT_MAX_HEADER_LIST_SIZE);
    Http2HeadersEncoder encoder =
        new DefaultHttp2HeadersEncoder(NEVER_SENSITIVE);

    Http2Headers headers = new DefaultHttp2Headers(false);
    headers.add(of(":status"), of("200")).add(of("custom"), of("header"));
    encodedHeaders = Unpooled.buffer();
    encoder.encodeHeaders(1 /* randomly chosen */, headers, encodedHeaders);

    Http2Headers decodedHeaders = decoder.decodeHeaders(3 /* randomly chosen */, encodedHeaders);
    assertEquals(headers.get(of(":status")), decodedHeaders.get(of(":status")));
    assertEquals(headers.get(of("custom")), decodedHeaders.get(of("custom")));
    assertEquals(headers.size(), decodedHeaders.size());

    String toString = decodedHeaders.toString();
    assertContainsKeyAndValue(toString, ":status", decodedHeaders.get(of(":status")));
    assertContainsKeyAndValue(toString, "custom", decodedHeaders.get(of("custom")));
  }

  @Test
  public void decode_emptyHeaders() throws Http2Exception {
    Http2HeadersDecoder decoder = new GrpcHttp2ClientHeadersDecoder(8192);
    Http2HeadersEncoder encoder =
        new DefaultHttp2HeadersEncoder(NEVER_SENSITIVE);

    ByteBuf encodedHeaders = Unpooled.buffer();
    encoder.encodeHeaders(1 /* randomly chosen */, new DefaultHttp2Headers(false), encodedHeaders);

    Http2Headers decodedHeaders = decoder.decodeHeaders(3 /* randomly chosen */, encodedHeaders);
    assertEquals(0, decodedHeaders.size());
    assertThat(decodedHeaders.toString(), containsString("[]"));
  }

  @Test
  public void dupBinHeadersWithComma() {
    Key<byte[]> key = Key.of("bytes-bin", BINARY_BYTE_MARSHALLER);
    Http2Headers http2Headers = new GrpcHttp2RequestHeaders(2);
    http2Headers.add(AsciiString.of("bytes-bin"), AsciiString.of("BaS,e6,,4+,padding=="));
    http2Headers.add(AsciiString.of("bytes-bin"), AsciiString.of("more"));
    http2Headers.add(AsciiString.of("bytes-bin"), AsciiString.of(""));
    Metadata recoveredHeaders = Utils.convertHeaders(http2Headers);
    byte[][] values = Iterables.toArray(recoveredHeaders.getAll(key), byte[].class);

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

  private static void assertContainsKeyAndValue(String str, CharSequence key, CharSequence value) {
    assertThat(str, containsString(key.toString()));
    assertThat(str, containsString(value.toString()));
  }
}
