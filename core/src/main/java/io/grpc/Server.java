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

package io.grpc;

import java.io.IOException;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.TimeUnit;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Server for listening for and dispatching incoming calls. It is not expected to be implemented by
 * application code or interceptors.
 */
@ThreadSafe
public abstract class Server {
  /**
   * Bind and start the server.
   *
   * @return {@code this} object
   * @throws IllegalStateException if already started
   * @throws IOException if unable to bind
   * @since 1.0.0
   */
  public abstract Server start() throws IOException;

  /**
   * Returns the port number the server is listening on.  This can return -1 if there is no actual
   * port or the result otherwise does not make sense.  Result is undefined after the server is
   * terminated.
   *
   * @throws IllegalStateException if the server has not yet been started.
   * @since 1.0.0
   */
  public int getPort() {
    return -1;
  }

  /**
   * Returns all services registered with the server, or an empty list if not supported by the
   * implementation.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public List<ServerServiceDefinition> getServices() {
    return Collections.emptyList();
  }

  /**
   * Returns immutable services registered with the server, or an empty list if not supported by the
   * implementation.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public List<ServerServiceDefinition> getImmutableServices() {
    return Collections.emptyList();
  }


  /**
   * Returns mutable services registered with the server, or an empty list if not supported by the
   * implementation.
   *
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public List<ServerServiceDefinition> getMutableServices() {
    return Collections.emptyList();
  }

  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are rejected.
   *
   * @return {@code this} object
   * @since 1.0.0
   */
  public abstract Server shutdown();

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are rejected. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   *
   * @return {@code this} object
   * @since 1.0.0
   */
  public abstract Server shutdownNow();

  /**
   * Returns whether the server is shutdown. Shutdown servers reject any new calls, but may still
   * have some calls being processed.
   *
   * @see #shutdown()
   * @see #isTerminated()
   * @since 1.0.0
   */
  public abstract boolean isShutdown();

  /**
   * Returns whether the server is terminated. Terminated servers have no running calls and
   * relevant resources released (like TCP connections).
   *
   * @see #isShutdown()
   * @since 1.0.0
   */
  public abstract boolean isTerminated();

  /**
   * Waits for the server to become terminated, giving up if the timeout is reached.
   *
   * @return whether the server is terminated, as would be done by {@link #isTerminated()}.
   */
  public abstract boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException;

  /**
   * Waits for the server to become terminated.
   *
   * @since 1.0.0
   */
  public abstract void awaitTermination() throws InterruptedException;
}
