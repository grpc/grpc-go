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
 * Interface to initiate processing of incoming remote calls. Advanced applications and generated
 * code will implement this interface to allows {@link Server}s to invoke service methods.
 */
@ThreadSafe
public interface ServerCallHandler<RequestT, ResponseT> {
  /**
   * Produce a non-{@code null} listener for the incoming call. Implementations are free to call
   * methods on {@code call} before this method has returned.
   *
   * <p>Since {@link Metadata} is not thread-safe, the caller must not access (read or write) {@code
   * headers} after this point.
   *
   * <p>If the implementation throws an exception, {@code call} will be closed with an error.
   * Implementations must not throw an exception if they started processing that may use {@code
   * call} on another thread.
   *
   * @param call object for responding to the remote client.
   * @return listener for processing incoming request messages for {@code call}
   */
  ServerCall.Listener<RequestT> startCall(
      ServerCall<RequestT, ResponseT> call,
      Metadata headers);
}
