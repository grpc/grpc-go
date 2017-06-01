/*
 * Copyright 2015, gRPC Authors All rights reserved.
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
