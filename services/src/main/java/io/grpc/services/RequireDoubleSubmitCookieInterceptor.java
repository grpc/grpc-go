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

import com.google.common.base.Objects;
import io.grpc.Metadata;
import io.grpc.Metadata.Key;
import io.grpc.ServerCall;
import io.grpc.ServerCall.Listener;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.Status;
import java.net.HttpCookie;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * An interceptor that checks for a double submit cookie, a form of XSRF protection. This
 * interceptor is intended for grpc-web based applications where the web page and grpc-web requests
 * are served from the same origin, namely behind a reverse proxy.
 *
 * <p>This interceptor works by requiring that for each RPC, a pseudo-random value session ID is
 * set as both a cookie as well as a request parameter in the form of a header. We rely on the
 * fact that web browsers only send a cookie when the origin and the cookie domain's
 * match, so RPCs invoked from other places can be detected and blocked.
 *
 * <p>This scheme requires the client app and server cooperate, and this interceptor implements
 * only the server side logic.
 *
 * <p>On the client side, the application is responsible for setting the cookie and the header.
 */
final class RequireDoubleSubmitCookieInterceptor implements ServerInterceptor {
  private static final Logger log
      = Logger.getLogger(RequireDoubleSubmitCookieInterceptor.class.getName());

  static final Key<String> COOKIE = Key.of("cookie", Metadata.ASCII_STRING_MARSHALLER);

  @SuppressWarnings("rawtypes")
  static final ServerCall.Listener NOOP = new ServerCall.Listener() {};

  private final String tokenName;
  private final Key<String> headerKey;
  private final Status failStatus;

  RequireDoubleSubmitCookieInterceptor(String tokenName) {
    this.tokenName = tokenName;
    headerKey = Key.of(tokenName, Metadata.ASCII_STRING_MARSHALLER);
    failStatus
        = Status.FAILED_PRECONDITION.withDescription(
            String.format("Double submit cookie failure. There must be both a cookie and "
                + "metadata with matching values, for XSRF protection. "
                + "The cookie and metadata keys must both be: %s", tokenName));
  }

  @SuppressWarnings("unchecked")
  private <ReqT, RespT> Listener<ReqT> failCall(ServerCall<ReqT, RespT> call) {
    call.close(failStatus, new Metadata());
    return NOOP;
  }

  @Override
  public <ReqT, RespT> Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> call, Metadata headers,
      ServerCallHandler<ReqT, RespT> next) {
    String xsrfCookie = null;
    Iterable<String> cookieHeaders = headers.getAll(COOKIE);
    if (cookieHeaders == null) {
      return failCall(call);
    }
    for (String cookieHeader : cookieHeaders) {
      try {
        for (HttpCookie cookie : HttpCookie.parse(cookieHeader)) {
          if (cookie.getName().equals(tokenName)) {
            if (xsrfCookie == null) {
              xsrfCookie = cookie.getValue();
            } else {
              log.log(Level.FINE, "Multiple cookies set for {}, this is not allowed", tokenName);
              return failCall(call);
            }
          }
        }
      } catch (IllegalArgumentException e) {
        log.log(Level.FINE, "Failed to parse cookie header", e);
        return failCall(call);
      }
    }
    String xsrfHeader = headers.get(headerKey);
    if (xsrfCookie == null || !Objects.equal(xsrfCookie, xsrfHeader)) {
      return failCall(call);
    }
    return next.startCall(call, headers);
  }
}
