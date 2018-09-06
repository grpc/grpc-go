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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.SettableFuture;
import io.grpc.CallOptions;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalLogId;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import java.util.concurrent.Executor;

/**
 * A client transport that creates streams that will immediately fail when started.
 */
class FailingClientTransport implements ClientTransport {
  @VisibleForTesting
  final Status error;
  private final RpcProgress rpcProgress;

  FailingClientTransport(Status error, RpcProgress rpcProgress) {
    Preconditions.checkArgument(!error.isOk(), "error must not be OK");
    this.error = error;
    this.rpcProgress = rpcProgress;
  }

  @Override
  public ClientStream newStream(
      MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions) {
    return new FailingClientStream(error, rpcProgress);
  }

  @Override
  public void ping(final PingCallback callback, Executor executor) {
    executor.execute(new Runnable() {
        @Override public void run() {
          callback.onFailure(error.asException());
        }
      });
  }

  @Override
  public ListenableFuture<SocketStats> getStats() {
    SettableFuture<SocketStats> ret = SettableFuture.create();
    ret.set(null);
    return ret;
  }

  @Override
  public InternalLogId getLogId() {
    throw new UnsupportedOperationException("Not a real transport");
  }
}
