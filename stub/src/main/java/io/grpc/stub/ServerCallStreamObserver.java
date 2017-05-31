/*
 * Copyright 2016, gRPC Authors All rights reserved.
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
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1788")
public abstract class ServerCallStreamObserver<V> extends CallStreamObserver<V> {

  /**
   * If {@code true} indicates that the call has been cancelled by the remote peer.
   */
  public abstract boolean isCancelled();

  /**
   * Set a {@link Runnable} that will be called if the calls  {@link #isCancelled()} state
   * changes from {@code false} to {@code true}. It is guaranteed that execution of the
   * {@link Runnable} are serialized with calls to the 'inbound' {@link StreamObserver}.
   *
   * <p>Note that the handler may be called some time after {@link #isCancelled} has transitioned to
   * {@code true} as other callbacks may still be executing in the 'inbound' observer.
   *
   * @param onCancelHandler to call when client has cancelled the call.
   */
  public abstract void setOnCancelHandler(Runnable onCancelHandler);

  /**
   * Sets the compression algorithm to use for the call.  May only be called before sending any
   * messages.
   *
   * @param compression the compression algorithm to use.
   */
  public abstract void setCompression(String compression);
}
