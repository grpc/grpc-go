/*
 * Copyright 2014, Google Inc. All rights reserved.
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

/**
 * Receives notifications from an observable stream of messages.
 *
 * <p>It is used by both the client stubs and service implementations for sending or receiving
 * stream messages. For outgoing messages, a {@code StreamObserver} is provided by the GRPC library
 * to the application. For incoming messages, the application implements the {@code StreamObserver}
 * and passes it to the GRPC library for receiving.
 *
 * <p>Implementations are expected to be thread-compatible. Separate {@code StreamObserver}s do
 * not need to be sychronized together; incoming and outgoing directions are independent.
 */
public interface StreamObserver<V>  {
  /**
   * Receives a value from the stream.
   *
   * <p>Can be called many times but is never called after {@link #onError(Throwable)} or {@link
   * #onCompleted()} are called.
   *
   * <p>If an exception is thrown by an implementation the caller is expected to terminate the
   * stream by calling {@link #onError(Throwable)} with the caught exception prior to
   * propagating it.
   *
   * @param value the value passed to the stream
   */
  public void onValue(V value);

  /**
   * Receives a terminating error from the stream.
   *
   * <p>May only be called once and is never called after {@link #onCompleted()}. In particular if
   * an exception is thrown by an implementation of {@code onError} no further calls to any
   * method are allowed.
   *
   * @param t the error occurred on the stream
   */
  public void onError(Throwable t);

  /**
   * Receives a notification of successful stream completion.
   *
   * <p>May only be called once and is never called after {@link #onError(Throwable)}. In particular
   * if an exception is thrown by an implementation of {@code onCompleted} no further calls to
   * any method are allowed.
   */
  public void onCompleted();
}
