/*
 * Copyright 2016 The gRPC Authors
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

final class MetadataApplierImpl extends MetadataApplier {
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
