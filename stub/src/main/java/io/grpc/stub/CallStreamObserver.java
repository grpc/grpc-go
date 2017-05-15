/*
 * Copyright 2016, Google Inc. All rights reserved.
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
   * <p>Note that for cases where the runtime knows that only one inbound message is allowed
   * calling this method will have no effect and the runtime will always permit one and only
   * one message. This is true for:
   * <ul>
   *   <li>{@link io.grpc.MethodDescriptor.MethodType#UNARY} operations on both the
   *   client and server.
   *   </li>
   *   <li>{@link io.grpc.MethodDescriptor.MethodType#CLIENT_STREAMING} operations on the server.
   *   </li>
   *   <li>{@link io.grpc.MethodDescriptor.MethodType#SERVER_STREAMING} operations on the client.
   *   </li>
   * </ul>
   * </p>
   */
  public abstract void disableAutoInboundFlowControl();

  /**
   * Requests the peer to produce {@code count} more messages to be delivered to the 'inbound'
   * {@link StreamObserver}.
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
