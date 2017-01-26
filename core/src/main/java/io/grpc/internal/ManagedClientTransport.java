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

package io.grpc.internal;

import io.grpc.Status;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * A {@link ClientTransport} that has life-cycle management.
 *
 * <p>{@link #start} must be the first method call to this interface and return before calling other
 * methods.
 *
 * <p>Typically the transport owns the streams it creates through {@link #newStream}, while some
 * implementations may transfer the streams to somewhere else. Either way they must conform to the
 * contract defined by {@link #shutdown}, {@link Listener#transportShutdown} and
 * {@link Listener#transportTerminated}.
 */
@ThreadSafe
public interface ManagedClientTransport extends ClientTransport, WithLogId {

  /**
   * Starts transport. This method may only be called once.
   *
   * <p>Implementations must not call {@code listener} from within {@link #start}; implementations
   * are expected to notify listener on a separate thread or when the returned {@link Runnable} is
   * run. This method and the returned {@code Runnable} should not throw any exceptions.
   *
   * @param listener non-{@code null} listener of transport events
   * @return a {@link Runnable} that is executed after-the-fact by the original caller, typically
   *     after locks are released
   */
  @CheckReturnValue
  @Nullable
  Runnable start(Listener listener);

  /**
   * Initiates an orderly shutdown of the transport.  Existing streams continue, but the transport
   * will not own any new streams.  New streams will either fail (once
   * {@link Listener#transportShutdown} callback called), or be transferred off this transport (in
   * which case they may succeed).  This method may only be called once.
   */
  void shutdown();

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are closed. Existing calls
   * should be closed with the provided {@code reason}.
   */
  void shutdownNow(Status reason);

  /**
   * Receives notifications for the transport life-cycle events. Implementation does not need to be
   * thread-safe, so notifications must be properly synchronized externally.
   */
  interface Listener {
    /**
     * The transport is shutting down. This transport will stop owning new streams, but existing
     * streams may continue, and the transport may still be able to process {@link #newStream} as
     * long as it doesn't own the new streams. Shutdown could have been caused by an error or normal
     * operation.  It is possible that this method is called without {@link #shutdown} being called.
     *
     * <p>This is called exactly once, and must be called prior to {@link #transportTerminated}.
     *
     * @param s the reason for the shutdown.
     */
    void transportShutdown(Status s);

    /**
     * The transport completed shutting down. All resources have been released. All streams have
     * either been closed or transferred off this transport. This transport may still be able to
     * process {@link #newStream} as long as it doesn't own the new streams.
     *
     * <p>This is called exactly once, and must be called after {@link #transportShutdown} has been
     * called.
     */
    void transportTerminated();

    /**
     * The transport is ready to accept traffic, because the connection is established.  This is
     * called at most once.
     */
    void transportReady();

    /**
     * Called whenever the transport's in-use state has changed. A transport is in-use when it has
     * at least one stream.
     */
    void transportInUse(boolean inUse);
  }
}
