/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.stub;

import io.grpc.ExperimentalApi;

import javax.annotation.Nullable;

/**
 * A refinement of {@link CallStreamObserver} that allows for lower-level interaction with
 * client calls.
 *
 * <p>Like {@code StreamObserver}, implementations are not required to be thread-safe; if multiple
 * threads will be writing to an instance concurrently, the application must synchronize its calls.
 *
 * <p>DO NOT MOCK: The API is too complex to reliably mock. Use InProcessChannelBuilder to create
 * "real" RPCs suitable for testing and make a fake for the server-side.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1788")
public abstract class ClientCallStreamObserver<V> extends CallStreamObserver<V> {
  /**
   * Prevent any further processing for this {@code ClientCallStreamObserver}. No further messages
   * will be received. The server is informed of cancellations, but may not stop processing the
   * call. Cancelling an already
   * {@code cancel()}ed {@code ClientCallStreamObserver} has no effect.
   *
   * <p>No other methods on this class can be called after this method has been called.
   *
   * <p>It is recommended that at least one of the arguments to be non-{@code null}, to provide
   * useful debug information. Both argument being null may log warnings and result in suboptimal
   * performance. Also note that the provided information will not be sent to the server.
   *
   * @param message if not {@code null}, will appear as the description of the CANCELLED status
   * @param cause if not {@code null}, will appear as the cause of the CANCELLED status
   */
  public abstract void cancel(@Nullable String message, @Nullable Throwable cause);
}
