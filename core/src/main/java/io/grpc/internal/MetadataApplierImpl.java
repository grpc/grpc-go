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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Preconditions.checkState;

import io.grpc.CallCredentials.MetadataApplier;
import io.grpc.CallOptions;
import io.grpc.Context;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

final class MetadataApplierImpl implements MetadataApplier {
  private final ClientTransport transport;
  private final MethodDescriptor<?, ?> method;
  private final Metadata origHeaders;
  private final CallOptions callOptions;
  private final Context ctx;

  private final Object lock = new Object();

  // null if neither apply() or returnStream() are called.
  // Needs this lock because apply() and returnStream() may race
  @GuardedBy("lock")
  @Nullable
  private ClientStream returnedStream;

  boolean finalized;

  // not null if returnStream() was called before apply()
  DelayedStream delayedStream;

  MetadataApplierImpl(
      ClientTransport transport, MethodDescriptor<?, ?> method, Metadata origHeaders,
      CallOptions callOptions) {
    this.transport = transport;
    this.method = method;
    this.origHeaders = origHeaders;
    this.callOptions = callOptions;
    this.ctx = Context.current();
  }

  @Override
  public void apply(Metadata headers) {
    checkState(!finalized, "apply() or fail() already called");
    checkNotNull(headers, "headers");
    origHeaders.merge(headers);
    ClientStream realStream;
    Context origCtx = ctx.attach();
    try {
      realStream = transport.newStream(method, origHeaders, callOptions);
    } finally {
      ctx.detach(origCtx);
    }
    finalizeWith(realStream);
  }

  @Override
  public void fail(Status status) {
    checkArgument(!status.isOk(), "Cannot fail with OK status");
    checkState(!finalized, "apply() or fail() already called");
    finalizeWith(new FailingClientStream(status));
  }

  private void finalizeWith(ClientStream stream) {
    checkState(!finalized, "already finalized");
    finalized = true;
    synchronized (lock) {
      if (returnedStream == null) {
        // Fast path: returnStream() hasn't been called, the call will use the
        // real stream directly.
        returnedStream = stream;
        return;
      }
    }
    // returnStream() has been called before me, thus delayedStream must have been
    // created.
    checkState(delayedStream != null, "delayedStream is null");
    delayedStream.setStream(stream);
  }

  /**
   * Return a stream on which the RPC will run on.
   */
  ClientStream returnStream() {
    synchronized (lock) {
      if (returnedStream == null) {
        // apply() has not been called, needs to buffer the requests.
        delayedStream = new DelayedStream();
        return returnedStream = delayedStream;
      } else {
        return returnedStream;
      }
    }
  }
}
