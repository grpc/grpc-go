/*
 * Copyright 2014 The gRPC Authors
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
   * @param call object to receive response messages
   * @param headers which can contain extra call metadata from {@link ClientCall#start},
   *                e.g. authentication credentials.
   * @param next next processor in the interceptor chain
   * @return listener for processing incoming messages for {@code call}, never {@code null}.
   */
  <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
      ServerCall<ReqT, RespT> call,
      Metadata headers,
      ServerCallHandler<ReqT, RespT> next);
}
