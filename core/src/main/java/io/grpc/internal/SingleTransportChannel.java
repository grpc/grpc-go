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

package io.grpc.internal;

import com.google.common.base.Preconditions;
import com.google.common.base.Stopwatch;
import com.google.common.base.Supplier;
import com.google.instrumentation.stats.StatsContextFactory;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;
import java.util.concurrent.Executor;
import java.util.concurrent.ScheduledExecutorService;

/**
 * A {@link Channel} that wraps a {@link ClientTransport}.
 */
final class SingleTransportChannel extends Channel {

  private final StatsContextFactory statsFactory;
  private final ClientTransport transport;
  private final Executor executor;
  private final String authority;
  private final ScheduledExecutorService deadlineCancellationExecutor;
  private final Supplier<Stopwatch> stopwatchSupplier;

  private final ClientTransportProvider transportProvider = new ClientTransportProvider() {
    @Override
    public ClientTransport get(CallOptions callOptions, Metadata headers) {
      return transport;
    }
  };

  /**
   * Creates a new channel with a connected transport.
   */
  public SingleTransportChannel(StatsContextFactory statsFactory, ClientTransport transport,
      Executor executor, ScheduledExecutorService deadlineCancellationExecutor, String authority,
      Supplier<Stopwatch> stopwatchSupplier) {
    this.statsFactory = Preconditions.checkNotNull(statsFactory, "statsFactory");
    this.transport = Preconditions.checkNotNull(transport, "transport");
    this.executor = Preconditions.checkNotNull(executor, "executor");
    this.deadlineCancellationExecutor = Preconditions.checkNotNull(
        deadlineCancellationExecutor, "deadlineCancellationExecutor");
    this.authority = Preconditions.checkNotNull(authority, "authority");
    this.stopwatchSupplier = Preconditions.checkNotNull(stopwatchSupplier, "stopwatchSupplier");
  }

  @Override
  public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
      MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
    StatsTraceContext statsTraceCtx = StatsTraceContext.newClientContext(
        methodDescriptor.getFullMethodName(), statsFactory, stopwatchSupplier);
    return new ClientCallImpl<RequestT, ResponseT>(methodDescriptor,
        new SerializingExecutor(executor), callOptions, statsTraceCtx, transportProvider,
        deadlineCancellationExecutor);
  }

  @Override
  public String authority() {
    return authority;
  }
}
