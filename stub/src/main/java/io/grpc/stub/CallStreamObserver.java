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
 * A refinement of StreamObserver provided by the GRPC runtime to the application that allows for
 * more complex interactions with call behavior.
 *
 * <p>In any call there are logically two {@link StreamObserver} implementations:
 * <ul>
 *   <li>'inbound' - which the GRPC runtime calls when it receives messages from the
 *   remote peer. This is implemented by the application.
 *   </li>
 *   <li>'outbound' - which the GRPC runtime provides to the application which it uses to
 *   send messages to the remote peer.
 *   </li>
 * </ul>
 *
 * <p>Implementations of this class represent the 'outbound' message stream.
 *
 * <p>Like {@code StreamObserver}, implementations are not required to be thread-safe; if multiple
 * threads will be writing to an instance concurrently, the application must synchronize its calls.
 *
 * <p>DO NOT MOCK: The API is too complex to reliably mock. Use InProcessChannelBuilder to create
 * "real" RPCs suitable for testing.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1788")
public abstract class CallStreamObserver<V> implements StreamObserver<V> {

  /**
   * If {@code true}, indicates that the observer is capable of sending additional messages
   * without requiring excessive buffering internally. This value is just a suggestion and the
   * application is free to ignore it, however doing so may result in excessive buffering within the
   * observer.
   */
  public abstract boolean isReady();

  /**
   * Set a {@link Runnable} that will be executed every time the stream {@link #isReady()} state
   * changes from {@code false} to {@code true}.  While it is not guaranteed that the same
   * thread will always be used to execute the {@link Runnable}, it is guaranteed that executions
   * are serialized with calls to the 'inbound' {@link StreamObserver}.
   *
   * <p>On client-side this method may only be called during {@link
   * ClientResponseObserver#beforeStart}. On server-side it may only be called during the initial
   * call to the application, before the service returns its {@code StreamObserver}.
   *
   * <p>Note that the handler may be called some time after {@link #isReady} has transitioned to
   * true as other callbacks may still be executing in the 'inbound' observer.
   *
   * @param onReadyHandler to call when peer is ready to receive more messages.
   */
  public abstract void setOnReadyHandler(Runnable onReadyHandler);

  /**
   * Disables automatic flow control where a token is returned to the peer after a call
   * to the 'inbound' {@link io.grpc.stub.StreamObserver#onNext(Object)} has completed. If disabled
   * an application must make explicit calls to {@link #request} to receive messages.
   *
   * <p>On client-side this method may only be called during {@link
   * ClientResponseObserver#beforeStart}. On server-side it may only be called during the initial
   * call to the application, before the service returns its {@code StreamObserver}.
   *
   * <p>Note that for cases where the runtime knows that only one inbound message is allowed
   * calling this method will have no effect and the runtime will always permit one and only
   * one message. This is true for:
   * <ul>
   *   <li>{@link io.grpc.MethodDescriptor.MethodType#UNARY} operations on both the
   *   client and server.
   *   </li>
   *   <li>{@link io.grpc.MethodDescriptor.MethodType#CLIENT_STREAMING} operations on the client.
   *   </li>
   *   <li>{@link io.grpc.MethodDescriptor.MethodType#SERVER_STREAMING} operations on the server.
   *   </li>
   * </ul>
   * </p>
   */
  public abstract void disableAutoInboundFlowControl();

  /**
   * Requests the peer to produce {@code count} more messages to be delivered to the 'inbound'
   * {@link StreamObserver}.
   *
   * <p>This method is safe to call from multiple threads without external synchronization.
   *
   * @param count more messages
   */
  public abstract void request(int count);

  /**
   * Sets message compression for subsequent calls to {@link #onNext}.
   *
   * @param enable whether to enable compression.
   */
  public abstract void setMessageCompression(boolean enable);
}
