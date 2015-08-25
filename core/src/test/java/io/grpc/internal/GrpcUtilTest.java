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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import io.grpc.Status;
import io.grpc.internal.GrpcUtil.Http2Error;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link GrpcUtil}. */
@RunWith(JUnit4.class)
public class GrpcUtilTest {
  @Test
  public void http2ErrorForCode() {
    // Try edge cases manually, to make the test obviously correct for important cases.
    assertNull(Http2Error.forCode(-1));
    assertSame(Http2Error.NO_ERROR, Http2Error.forCode(0));
    assertSame(Http2Error.HTTP_1_1_REQUIRED, Http2Error.forCode(0xD));
    assertNull(Http2Error.forCode(0xD + 1));
  }

  @Test
  public void http2ErrorRoundTrip() {
    for (Http2Error error : Http2Error.values()) {
      assertSame(error, Http2Error.forCode(error.code()));
    }
  }

  @Test
  public void http2ErrorStatus() {
    // Nothing special about this particular error, except that it is slightly distinctive.
    assertSame(Status.Code.CANCELLED, Http2Error.CANCEL.status().getCode());
  }

  @Test
  public void http2ErrorStatusForCode() {
    assertSame(Status.Code.INTERNAL, Http2Error.statusForCode(-1).getCode());
    assertSame(Http2Error.NO_ERROR.status(), Http2Error.statusForCode(0));
    assertSame(Http2Error.HTTP_1_1_REQUIRED.status(), Http2Error.statusForCode(0xD));
    assertSame(Status.Code.INTERNAL, Http2Error.statusForCode(0xD + 1).getCode());
  }

  @Test
  public void timeoutTest() {
    GrpcUtil.TimeoutMarshaller marshaller =
            new GrpcUtil.TimeoutMarshaller();
    assertEquals("1000u", marshaller.toAsciiString(1000L));
    assertEquals(1000L, (long) marshaller.parseAsciiString("1000u"));

    assertEquals("100000m", marshaller.toAsciiString(100000000L));
    assertEquals(100000000L, (long) marshaller.parseAsciiString("100000m"));

    assertEquals("100000S", marshaller.toAsciiString(100000000000L));
    assertEquals(100000000000L, (long) marshaller.parseAsciiString("100000S"));

    // 1,666,667 * 60 has 9 digits
    assertEquals("1666666M", marshaller.toAsciiString(100000000000000L));
    assertEquals(60000000000000L, (long) marshaller.parseAsciiString("1000000M"));

    // 1,666,667 * 60 has 9 digits
    assertEquals("1666666H", marshaller.toAsciiString(6000000000000000L));
    assertEquals(3600000000000000L, (long) marshaller.parseAsciiString("1000000H"));
  }

  @Test
  public void grpcUserAgent() {
    assertTrue(GrpcUtil.getGrpcUserAgent("netty", null).startsWith("grpc-java-netty"));
    assertTrue(GrpcUtil.getGrpcUserAgent("okhttp", "libfoo/1.0")
        .startsWith("libfoo/1.0 grpc-java-okhttp"));
  }

  @Test
  public void contentTypeShouldBeValid() {
    assertTrue(GrpcUtil.isGrpcContentType(GrpcUtil.CONTENT_TYPE_GRPC));
    assertTrue(GrpcUtil.isGrpcContentType(GrpcUtil.CONTENT_TYPE_GRPC + "+blaa"));
    assertTrue(GrpcUtil.isGrpcContentType(GrpcUtil.CONTENT_TYPE_GRPC + ";blaa"));
  }

  @Test
  public void contentTypeShouldNotBeValid() {
    assertFalse(GrpcUtil.isGrpcContentType("application/bad"));
  }
}
