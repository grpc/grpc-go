/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.netty;

import static io.netty.handler.codec.http2.Http2CodecUtil.DEFAULT_HEADER_TABLE_SIZE;
import static io.netty.util.AsciiString.of;
import static org.junit.Assert.assertEquals;

import io.grpc.netty.GrpcHttp2HeadersDecoder.GrpcHttp2ClientHeadersDecoder;
import io.grpc.netty.GrpcHttp2HeadersDecoder.GrpcHttp2ServerHeadersDecoder;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.DefaultHttp2HeadersEncoder;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2HeadersDecoder;
import io.netty.handler.codec.http2.Http2HeadersEncoder;
import io.netty.handler.codec.http2.Http2HeadersEncoder.SensitivityDetector;
import io.netty.util.ReferenceCountUtil;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link GrpcHttp2HeadersDecoder}.
 */
@RunWith(JUnit4.class)
public class GrpcHttp2HeadersDecoderTest {

  private static final SensitivityDetector NEVER_SENSITIVE = new SensitivityDetector() {
    @Override
    public boolean isSensitive(CharSequence name, CharSequence value) {
      return false;
    }
  };

  @Test
  public void decode_requestHeaders() throws Http2Exception {
    Http2HeadersDecoder decoder = new GrpcHttp2ServerHeadersDecoder(8192);
    Http2HeadersEncoder encoder =
        new DefaultHttp2HeadersEncoder(DEFAULT_HEADER_TABLE_SIZE, NEVER_SENSITIVE);

    Http2Headers headers = new DefaultHttp2Headers(false);
    headers.add(of(":scheme"), of("https")).add(of(":method"), of("GET"))
        .add(of(":path"), of("index.html")).add(of(":authority"), of("foo.grpc.io"))
        .add(of("custom"), of("header"));
    ByteBuf encodedHeaders = ReferenceCountUtil.releaseLater(Unpooled.buffer());
    encoder.encodeHeaders(headers, encodedHeaders);

    Http2Headers decodedHeaders = decoder.decodeHeaders(encodedHeaders);
    assertEquals(headers.get(of(":scheme")), decodedHeaders.scheme());
    assertEquals(headers.get(of(":method")), decodedHeaders.method());
    assertEquals(headers.get(of(":path")), decodedHeaders.path());
    assertEquals(headers.get(of(":authority")), decodedHeaders.authority());
    assertEquals(headers.get(of("custom")), decodedHeaders.get(of("custom")));
    assertEquals(headers.size(), decodedHeaders.size());
  }

  @Test
  public void decode_responseHeaders() throws Http2Exception {
    Http2HeadersDecoder decoder = new GrpcHttp2ClientHeadersDecoder(8192);
    Http2HeadersEncoder encoder =
        new DefaultHttp2HeadersEncoder(DEFAULT_HEADER_TABLE_SIZE, NEVER_SENSITIVE);

    Http2Headers headers = new DefaultHttp2Headers(false);
    headers.add(of(":status"), of("200")).add(of("custom"), of("header"));
    ByteBuf encodedHeaders = ReferenceCountUtil.releaseLater(Unpooled.buffer());
    encoder.encodeHeaders(headers, encodedHeaders);

    Http2Headers decodedHeaders = decoder.decodeHeaders(encodedHeaders);
    assertEquals(headers.get(of(":status")), decodedHeaders.get(of(":status")));
    assertEquals(headers.get(of("custom")), decodedHeaders.get(of("custom")));
    assertEquals(headers.size(), decodedHeaders.size());
  }
}
