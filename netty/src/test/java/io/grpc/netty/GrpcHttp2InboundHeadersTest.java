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

import static io.grpc.InternalMetadata.BASE64_ENCODING_OMIT_PADDING;
import static io.netty.util.AsciiString.of;
import static junit.framework.TestCase.assertNotSame;
import static org.junit.Assert.assertArrayEquals;
import static org.junit.Assert.assertEquals;

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
@SuppressWarnings({ "BadImport", "UndefinedEquals" }) // AsciiString.of and AsciiString.equals
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
    headers.add(of("foo-bin"), of(BASE64_ENCODING_OMIT_PADDING.encode(data)));

    assertEquals(1, headers.size());

    byte[][] namesAndValues = ((GrpcHttp2InboundHeaders)headers).namesAndValues();

    assertEquals(of("foo-bin"), new AsciiString(namesAndValues[0]));
    assertNotSame(data, namesAndValues[1]);
    assertArrayEquals(data, namesAndValues[1]);
  }

}
