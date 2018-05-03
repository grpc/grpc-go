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
 * A virtual connection to a conceptual endpoint, to perform RPCs. A channel is free to have zero or
 * many actual connections to the endpoint based on configuration, load, etc. A channel is also free
 * to determine which actual endpoints to use and may change it every RPC, permitting client-side
 * load balancing. Applications are generally expected to use stubs instead of calling this class
 * directly.
 *
 * <p>Applications can add common cross-cutting behaviors to stubs by decorating Channel
 * implementations using {@link ClientInterceptor}. It is expected that most application
 * code will not use this class directly but rather work with stubs that have been bound to a
 * Channel that was decorated during application initialization.
 */
@ThreadSafe
public abstract class Channel {
  /**
   * Create a {@link ClientCall} to the remote operation specified by the given
   * {@link MethodDescriptor}. The returned {@link ClientCall} does not trigger any remote
   * behavior until {@link ClientCall#start(ClientCall.Listener, Metadata)} is
   * invoked.
   *
   * @param methodDescriptor describes the name and parameter types of the operation to call.
   * @param callOptions runtime options to be applied to this call.
   * @return a {@link ClientCall} bound to the specified method.
   * @since 1.0.0
   */
  public abstract <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
      MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions);

  /**
   * The authority of the destination this channel connects to. Typically this is in the format
   * {@code host:port}.
   *
   * @since 1.0.0
   */
  public abstract String authority();
}
