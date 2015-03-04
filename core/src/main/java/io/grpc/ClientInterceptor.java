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
 * Interface for intercepting outgoing calls before they are dispatched by a {@link Channel}.
 *
 * <p>Implementers use this mechanism to add cross-cutting behavior to {@link Channel} and
 * stub implementations. Common examples of such behavior include:
 * <ul>
 * <li>Adding credentials to header metadata</li>
 * <li>Logging and monitoring call behavior</li>
 * <li>Request and response rewriting</li>
 * </ul>
 */
@ThreadSafe
public interface ClientInterceptor {
  /**
   * Intercept {@link Call} creation by the {@code next} {@link Channel}.
   * <p>
   * Many variations of interception are possible. Complex implementations may return a wrapper
   * around the result of {@code next.newCall()}, whereas a simpler implementation may just modify
   * the header metadata prior to returning the result of {@code next.newCall()}.
   *
   * @param method the remote method to be called.
   * @param next the channel which is being intercepted.
   * @return the call object for the remote operation, never {@code null}.
   */
  <RequestT, ResponseT> Call<RequestT, ResponseT> interceptCall(
      MethodDescriptor<RequestT, ResponseT> method,
      Channel next);
}
