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

package com.google.net.stubby.transport;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.AbstractService;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.MethodDescriptor;

/**
 * Abstract base class for all {@link ClientTransport} implementations. Implements the
 * {@link #newStream} method to perform a state check on the service before allowing stream
 * creation.
 */
public abstract class AbstractClientTransport extends AbstractService implements ClientTransport {

  @Override
  public final ClientStream newStream(MethodDescriptor<?, ?> method,
                                      Metadata.Headers headers,
                                      ClientStreamListener listener) {
    Preconditions.checkNotNull(method, "method");
    Preconditions.checkNotNull(listener, "listener");
    if (state() == State.STARTING) {
      // Wait until the transport is running before creating the new stream.
      awaitRunning();
    }

    if (state() != State.RUNNING) {
      throw new IllegalStateException("Invalid state for creating new stream: " + state(),
          failureCause());
    }

    // Create the stream.
    return newStreamInternal(method, headers, listener);
  }

  /**
   * Called by {@link #newStream} to perform the actual creation of the new {@link ClientStream}.
   * This is only called after the transport has successfully transitioned to the {@code RUNNING}
   * state.
   *
   * @param method the RPC method to be invoked on the server by the new stream.
   * @param listener the listener for events on the new stream.
   * @return the new stream.
   */
  protected abstract ClientStream newStreamInternal(MethodDescriptor<?, ?> method,
      Metadata.Headers headers,
      ClientStreamListener listener);
}
