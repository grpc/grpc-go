/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;

import io.grpc.Status;
import io.netty.channel.ConnectTimeoutException;
import io.netty.handler.codec.http2.Http2Error;
import io.netty.handler.codec.http2.Http2Exception;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link Utils}. */
@RunWith(JUnit4.class)
public class UtilsTest {
  @Test
  public void testStatusFromThrowable() {
    Status s = Status.CANCELLED.withDescription("msg");
    assertSame(s, Utils.statusFromThrowable(new Exception(s.asException())));
    Throwable t;
    t = new ConnectTimeoutException("msg");
    assertStatusEquals(Status.UNAVAILABLE.withCause(t), Utils.statusFromThrowable(t));
    t = new Http2Exception(Http2Error.INTERNAL_ERROR, "msg");
    assertStatusEquals(Status.INTERNAL.withCause(t), Utils.statusFromThrowable(t));
    t = new Exception("msg");
    assertStatusEquals(Status.UNKNOWN.withCause(t), Utils.statusFromThrowable(t));
  }

  private static void assertStatusEquals(Status expected, Status actual) {
    assertEquals(expected.getCode(), actual.getCode());
    assertEquals(expected.getDescription(), actual.getDescription());
    assertEquals(expected.getCause(), actual.getCause());
  }
}
