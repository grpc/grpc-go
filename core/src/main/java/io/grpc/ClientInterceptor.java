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
   * Intercept {@link ClientCall} creation by the {@code next} {@link Channel}.
   *
   * <p>Many variations of interception are possible. Complex implementations may return a wrapper
   * around the result of {@code next.newCall()}, whereas a simpler implementation may just modify
   * the header metadata prior to returning the result of {@code next.newCall()}.
   *
   * <p>{@code next.newCall()} <strong>must not</strong> be called under a different {@link Context}
   * other than the current {@code Context}. The outcome of such usage is undefined and may cause
   * memory leak due to unbounded chain of {@code Context}s.
   *
   * @param method the remote method to be called.
   * @param callOptions the runtime options to be applied to this call.
   * @param next the channel which is being intercepted.
   * @return the call object for the remote operation, never {@code null}.
   */
  <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, Channel next);
}
