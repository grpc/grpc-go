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
