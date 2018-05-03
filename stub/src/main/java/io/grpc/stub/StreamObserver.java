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

package io.grpc.stub;

/**
 * Receives notifications from an observable stream of messages.
 *
 * <p>It is used by both the client stubs and service implementations for sending or receiving
 * stream messages. It is used for all {@link io.grpc.MethodDescriptor.MethodType}, including
 * {@code UNARY} calls.  For outgoing messages, a {@code StreamObserver} is provided by the GRPC
 * library to the application. For incoming messages, the application implements the
 * {@code StreamObserver} and passes it to the GRPC library for receiving.
 *
 * <p>Implementations are not required to be thread-safe (but should be
 * <a href="http://www.ibm.com/developerworks/library/j-jtp09263/">thread-compatible</a>).
 * Separate {@code StreamObserver}s do
 * not need to be synchronized together; incoming and outgoing directions are independent.
 * Since individual {@code StreamObserver}s are not thread-safe, if multiple threads will be
 * writing to a {@code StreamObserver} concurrently, the application must synchronize calls.
 */
public interface StreamObserver<V>  {
  /**
   * Receives a value from the stream.
   *
   * <p>Can be called many times but is never called after {@link #onError(Throwable)} or {@link
   * #onCompleted()} are called.
   *
   * <p>Unary calls must invoke onNext at most once.  Clients may invoke onNext at most once for
   * server streaming calls, but may receive many onNext callbacks.  Servers may invoke onNext at
   * most once for client streaming calls, but may receive many onNext callbacks.
   *
   * <p>If an exception is thrown by an implementation the caller is expected to terminate the
   * stream by calling {@link #onError(Throwable)} with the caught exception prior to
   * propagating it.
   *
   * @param value the value passed to the stream
   */
  void onNext(V value);

  /**
   * Receives a terminating error from the stream.
   *
   * <p>May only be called once and if called it must be the last method called. In particular if an
   * exception is thrown by an implementation of {@code onError} no further calls to any method are
   * allowed.
   *
   * <p>{@code t} should be a {@link io.grpc.StatusException} or {@link
   * io.grpc.StatusRuntimeException}, but other {@code Throwable} types are possible. Callers should
   * generally convert from a {@link io.grpc.Status} via {@link io.grpc.Status#asException()} or
   * {@link io.grpc.Status#asRuntimeException()}. Implementations should generally convert to a
   * {@code Status} via {@link io.grpc.Status#fromThrowable(Throwable)}.
   *
   * @param t the error occurred on the stream
   */
  void onError(Throwable t);

  /**
   * Receives a notification of successful stream completion.
   *
   * <p>May only be called once and if called it must be the last method called. In particular if an
   * exception is thrown by an implementation of {@code onCompleted} no further calls to any method
   * are allowed.
   */
  void onCompleted();
}
