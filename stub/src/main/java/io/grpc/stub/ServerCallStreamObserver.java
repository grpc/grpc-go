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

/**
 * A refinement of {@link CallStreamObserver} to allows for interaction with call
 * cancellation events on the server side.
 *
 * <p>Like {@code StreamObserver}, implementations are not required to be thread-safe; if multiple
 * threads will be writing to an instance concurrently, the application must synchronize its calls.
 *
 * <p>DO NOT MOCK: The API is too complex to reliably mock. Use InProcessChannelBuilder to create
 * "real" RPCs suitable for testing and interact with the server using a normal client stub.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1788")
public abstract class ServerCallStreamObserver<V> extends CallStreamObserver<V> {

  /**
   * If {@code true} indicates that the call has been cancelled by the remote peer.
   *
   * <p>This method may safely be called concurrently from multiple threads.
   */
  public abstract boolean isCancelled();

  /**
   * Set a {@link Runnable} that will be called if the calls {@link #isCancelled()} state
   * changes from {@code false} to {@code true}. It is guaranteed that execution of the
   * {@link Runnable} are serialized with calls to the 'inbound' {@link StreamObserver}.
   *
   * <p>Note that the handler may be called some time after {@link #isCancelled()} has transitioned
   * to {@code true} as other callbacks may still be executing in the 'inbound' observer.
   *
   * <p>Setting the onCancelHandler will suppress the on-cancel exception thrown by
   * {@link #onNext}.
   *
   * @param onCancelHandler to call when client has cancelled the call.
   */
  public abstract void setOnCancelHandler(Runnable onCancelHandler);

  /**
   * Sets the compression algorithm to use for the call. May only be called before sending any
   * messages. Default gRPC servers support the "gzip" compressor.
   *
   * <p>It is safe to call this even if the client does not support the compression format chosen.
   * The implementation will handle negotiation with the client and may fall back to no compression.
   *
   * @param compression the compression algorithm to use.
   * @throws IllegalArgumentException if the compressor name can not be found.
   */
  public abstract void setCompression(String compression);
}
