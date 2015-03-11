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

import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;

import io.grpc.Status;
import io.grpc.transport.HttpUtil.Http2Error;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link HttpUtil}. */
@RunWith(JUnit4.class)
public class HttpUtilTest {
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
}
