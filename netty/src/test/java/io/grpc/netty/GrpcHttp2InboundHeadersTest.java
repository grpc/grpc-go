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

import static io.netty.util.AsciiString.of;
import static junit.framework.TestCase.assertNotSame;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;

import com.google.common.io.BaseEncoding;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2InboundHeaders;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2RequestHeaders;
import io.grpc.netty.GrpcHttp2HeadersUtils.GrpcHttp2ResponseHeaders;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.util.AsciiString;
import java.util.Random;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link GrpcHttp2RequestHeaders} and {@link GrpcHttp2ResponseHeaders}.
 */
@RunWith(JUnit4.class)
public class GrpcHttp2InboundHeadersTest {

  @Test
  public void basicCorrectness() {
    Http2Headers headers = new GrpcHttp2RequestHeaders(1);
    headers.add(of(":method"), of("POST"));
    headers.add(of("content-type"), of("application/grpc+proto"));
    headers.add(of(":path"), of("/google.pubsub.v2.PublisherService/CreateTopic"));
    headers.add(of(":scheme"), of("https"));
    headers.add(of("te"), of("trailers"));
    headers.add(of(":authority"), of("pubsub.googleapis.com"));
    headers.add(of("foo"), of("bar"));

    assertEquals(7, headers.size());
    // Number of headers without the pseudo headers and 'te' header.
    assertEquals(2, ((GrpcHttp2InboundHeaders)headers).numHeaders());

    assertEquals(of("application/grpc+proto"), headers.get(of("content-type")));
    assertEquals(of("/google.pubsub.v2.PublisherService/CreateTopic"), headers.path());
    assertEquals(of("https"), headers.scheme());
    assertEquals(of("POST"), headers.method());
    assertEquals(of("pubsub.googleapis.com"), headers.authority());
    assertEquals(of("trailers"), headers.get(of("te")));
    assertEquals(of("bar"), headers.get(of("foo")));
  }

  @Test
  public void binaryHeadersShouldBeBase64Decoded() {
    Http2Headers headers = new GrpcHttp2RequestHeaders(1);

    byte[] data = new byte[100];
    new Random().nextBytes(data);
    headers.add(of("foo-bin"), of(BaseEncoding.base64().encode(data)));

    assertEquals(1, headers.size());

    byte[][] namesAndValues = ((GrpcHttp2InboundHeaders)headers).namesAndValues();

    assertEquals(of("foo-bin"), new AsciiString(namesAndValues[0]));
    assertNotSame(data, namesAndValues[1]);
    assertArrayEquals(data, namesAndValues[1]);
  }

}
