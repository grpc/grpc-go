/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.services;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;

import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.Status;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link RequireDoubleSubmitCookieInterceptor}. */
@RunWith(JUnit4.class)
public final class RequireDoubleSubmitCookieInterceptorTest {
  private static final String XSRF_TOKEN_VALUE = "abc123";
  private static final ServerCall.Listener<Void> NEXT_LISTENER = new ServerCall.Listener<Void>() {};
  private static final ServerCallHandler<Void, Void> NEXT = new ServerCallHandler<Void, Void>() {
    @Override
    public Listener<Void> startCall(ServerCall<Void, Void> call, Metadata headers) {
      return NEXT_LISTENER;
    }
  };

  private Status closeStatus;
  private final ServerCall<Void, Void> call = new ServerCall<Void, Void>() {
    @Override
    public void close(Status status, Metadata trailers) {
      closeStatus = status;
    }

    @Override
    public void request(int numMessages) {}

    @Override
    public void sendHeaders(Metadata headers) {}

    @Override
    public void sendMessage(Void message) {}

    @Override
    public boolean isCancelled() {
      return false;
    }

    @Override
    public MethodDescriptor<Void, Void> getMethodDescriptor() {
      return null;
    }
  };

  private final RequireDoubleSubmitCookieInterceptor interceptor
      = new RequireDoubleSubmitCookieInterceptor("test-xsrf-token");
  private final Metadata.Key<String> xsrfHeader
      = Metadata.Key.of("test-xsrf-token", Metadata.ASCII_STRING_MARSHALLER);

  @Test
  public void noCookieNoHeader() throws Exception {
    Metadata headers = new Metadata();
    Listener<Void> listener = interceptor.interceptCall(call, headers, NEXT);
    assertSame(RequireDoubleSubmitCookieInterceptor.NOOP, listener);
    assertEquals(Status.FAILED_PRECONDITION.getCode(), closeStatus.getCode());
  }

  @Test
  public void noHeader() throws Exception {
    Metadata headers = new Metadata();
    Listener<Void> listener = interceptor.interceptCall(call, headers, NEXT);
    headers.put(RequireDoubleSubmitCookieInterceptor.COOKIE, "test-xsrf-token=" + XSRF_TOKEN_VALUE);
    assertSame(RequireDoubleSubmitCookieInterceptor.NOOP, listener);
    assertEquals(Status.FAILED_PRECONDITION.getCode(), closeStatus.getCode());
  }

  @Test
  public void noCookie() throws Exception {
    Metadata headers = new Metadata();
    Listener<Void> listener = interceptor.interceptCall(call, headers, NEXT);
    headers.put(xsrfHeader, XSRF_TOKEN_VALUE);
    assertSame(RequireDoubleSubmitCookieInterceptor.NOOP, listener);
    assertEquals(Status.FAILED_PRECONDITION.getCode(), closeStatus.getCode());
  }

  @Test
  public void matchingCookieAndHeader() throws Exception {
    Metadata headers = new Metadata();
    headers.put(
        RequireDoubleSubmitCookieInterceptor.COOKIE, "test-xsrf-token="  + XSRF_TOKEN_VALUE);
    headers.put(xsrfHeader, XSRF_TOKEN_VALUE);
    Listener<Void> listener = interceptor.interceptCall(call, headers, NEXT);
    assertSame(NEXT_LISTENER, listener);
    assertNull(closeStatus);
  }

  @Test
  public void mismatchedCookieAndHeader() throws Exception {
    Metadata headers = new Metadata();
    headers.put(
        RequireDoubleSubmitCookieInterceptor.COOKIE, "test-xsrf-token="  + XSRF_TOKEN_VALUE);
    headers.put(xsrfHeader, "foobar");
    Listener<Void> listener = interceptor.interceptCall(call, headers, NEXT);
    assertSame(RequireDoubleSubmitCookieInterceptor.NOOP, listener);
    assertEquals(Status.FAILED_PRECONDITION.getCode(), closeStatus.getCode());
  }
}
