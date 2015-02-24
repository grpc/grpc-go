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

package io.grpc;

import javax.annotation.concurrent.ThreadSafe;

/**
 * Interface for intercepting incoming calls before that are dispatched by
 * {@link ServerCallHandler}.
 *
 * <p>Implementers use this mechanism to add cross-cutting behavior to server-side calls. Common
 * example of such behavior include:
 * <ul>
 * <li>Enforcing valid authentication credentials</li>
 * <li>Logging and monitoring call behavior</li>
 * <li>Delegating calls to other servers</li>
 * </ul>
 */
@ThreadSafe
public interface ServerInterceptor {
  /**
   * Intercept {@link ServerCall} dispatch by the {@code next} {@link ServerCallHandler}. General
   * semantics of {@link ServerCallHandler#startCall} apply and the returned
   * {@link io.grpc.ServerCall.Listener} must not be {@code null}.
   *
   * <p>If the implementation throws an exception, {@code call} will be closed with an error.
   * Implementations must not throw an exception if they started processing that may use {@code
   * call} on another thread.
   *
   * @param method fully qualified method name of the call
   * @param call object to receive response messages
   * @param next next processor in the interceptor chain
   * @return listener for processing incoming messages for {@code call}, never {@code null}.
   */
  <RequestT, ResponseT> ServerCall.Listener<RequestT> interceptCall(
      String method,
      ServerCall<ResponseT> call,
      Metadata.Headers headers,
      ServerCallHandler<RequestT, ResponseT> next);
}
