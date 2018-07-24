/*
 * Copyright 2015 The gRPC Authors
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
   * <p>Note that the core library did not provide an implementation of this method until v1.6.1.
   *
   * @param requestConnection if {@code true}, the channel will try to make a connection if it is
   *        currently IDLE
   * @throws UnsupportedOperationException if not supported by implementation
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4359")
  public ConnectivityState getState(boolean requestConnection) {
    throw new UnsupportedOperationException("Not implemented");
  }

  /**
   * Registers a one-off callback that will be run if the connectivity state of the channel diverges
   * from the given {@code source}, which is typically what has just been returned by {@link
   * #getState}.  If the states are already different, the callback will be called immediately.  The
   * callback is run in the same executor that runs Call listeners.
   *
   * <p>There is an inherent race between the notification to {@code callback} and any call to
   * {@code getState()}. There is a similar race between {@code getState()} and a call to {@code
   * notifyWhenStateChanged()}. The state can change during those races, so there is not a way to
   * see every state transition. "Transitions" to the same state are possible, because intermediate
   * states may not have been observed. The API is only reliable in tracking the <em>current</em>
   * state.
   *
   * <p>Note that the core library did not provide an implementation of this method until v1.6.1.
   *
   * @param source the assumed current state, typically just returned by {@link #getState}
   * @param callback the one-off callback
   * @throws UnsupportedOperationException if not supported by implementation
   * @since 1.1.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4359")
  public void notifyWhenStateChanged(ConnectivityState source, Runnable callback) {
    throw new UnsupportedOperationException("Not implemented");
  }

  /**
   * For subchannels that are in TRANSIENT_FAILURE state, short-circuit the backoff timer and make
   * them reconnect immediately. May also attempt to invoke {@link NameResolver#refresh}.
   *
   * <p>This is primarily intended for Android users, where the network may experience frequent
   * temporary drops. Rather than waiting for gRPC's name resolution and reconnect timers to elapse
   * before reconnecting, the app may use this method as a mechanism to notify gRPC that the network
   * is now available and a reconnection attempt may occur immediately.
   *
   * <p>No-op if not supported by the implementation.
   *
   * @since 1.8.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4056")
  public void resetConnectBackoff() {}

  /**
   * Invoking this method moves the channel into the IDLE state and triggers tear-down of the
   * channel's name resolver and load balancer, while still allowing on-going RPCs on the channel to
   * continue. New RPCs on the channel will trigger creation of a new connection.
   *
   * <p>This is primarily intended for Android users when a device is transitioning from a cellular
   * to a wifi connection. The OS will issue a notification that a new network (wifi) has been made
   * the default, but for approximately 30 seconds the device will maintain both the cellular
   * and wifi connections. Apps may invoke this method to ensure that new RPCs are created using the
   * new default wifi network, rather than the soon-to-be-disconnected cellular network.
   *
   * <p>No-op if not supported by implementation.
   *
   * @since 1.11.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4056")
  public void enterIdle() {}
}
