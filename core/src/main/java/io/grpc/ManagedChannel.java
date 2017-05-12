/*
 * Copyright 2015, Google Inc. All rights reserved.
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

import java.util.concurrent.TimeUnit;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A {@link Channel} that provides lifecycle management.
 */
@ThreadSafe
public abstract class ManagedChannel extends Channel {
  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are immediately
   * cancelled.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract ManagedChannel shutdown();

  /**
   * Returns whether the channel is shutdown. Shutdown channels immediately cancel any new calls,
   * but may still have some calls being processed.
   *
   * @see #shutdown()
   * @see #isTerminated()
   * @since 1.0.0
   */
  public abstract boolean isShutdown();

  /**
   * Returns whether the channel is terminated. Terminated channels have no running calls and
   * relevant resources released (like TCP connections).
   *
   * @see #isShutdown()
   * @since 1.0.0
   */
  public abstract boolean isTerminated();

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are cancelled. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   *
   * @return this
   * @since 1.0.0
   */
  public abstract ManagedChannel shutdownNow();

  /**
   * Waits for the channel to become terminated, giving up if the timeout is reached.
   *
   * @return whether the channel is terminated, as would be done by {@link #isTerminated()}.
   * @since 1.0.0
   */
  public abstract boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException;

  /**
   * Gets the current connectivity state. Note the result may soon become outdated.
   *
   * <p><strong>Warning</strong>: this API is not yet implemented by the gRPC library (<a
   * href="https://github.com/grpc/grpc-java/issues/2292" target="_blank">issue on github</a>).
   *
   * @param requestConnection if {@code true}, the channel will try to make a connection if it is
   *        currently IDLE
   * @throws UnsupportedOperationException if not supported by implementation
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/28")
  public ConnectivityState getState(boolean requestConnection) {
    throw new UnsupportedOperationException("Not implemented");
  }

  /**
   * Registers a one-off callback that will be run if the connectivity state of the channel diverges
   * from the given {@code source}, which is typically what has just been returned by {@link
   * #getState}.  If the states are already different, the callback will be called immediately.  The
   * callback is run in the same executor that runs Call listeners.
   *
   * <p><strong>Warning</strong>: this API is not yet implemented by the gRPC library (<a
   * href="https://github.com/grpc/grpc-java/issues/2292" target="_blank">issue on github</a>).
   *
   * @param source the assumed current state, typically just returned by {@link #getState}
   * @param callback the one-off callback
   * @throws UnsupportedOperationException if not supported by implementation
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/28")
  public void notifyWhenStateChanged(ConnectivityState source, Runnable callback) {
    throw new UnsupportedOperationException("Not implemented");
  }
}
