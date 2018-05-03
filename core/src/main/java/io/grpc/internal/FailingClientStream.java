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
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener.RpcProgress;

/**
 * An implementation of {@link ClientStream} that fails (by calling {@link
 * ClientStreamListener#closed}) when started, and silently does nothing for the other operations.
 */
public final class FailingClientStream extends NoopClientStream {
  private boolean started;
  private final Status error;
  private final RpcProgress rpcProgress;

  /**
   * Creates a {@code FailingClientStream} that would fail with the given error.
   */
  public FailingClientStream(Status error) {
    this(error, RpcProgress.PROCESSED);
  }

  /**
   * Creates a {@code FailingClientStream} that would fail with the given error.
   */
  public FailingClientStream(Status error, RpcProgress rpcProgress) {
    Preconditions.checkArgument(!error.isOk(), "error must not be OK");
    this.error = error;
    this.rpcProgress = rpcProgress;
  }

  @Override
  public void start(ClientStreamListener listener) {
    Preconditions.checkState(!started, "already started");
    started = true;
    listener.closed(error, rpcProgress, new Metadata());
  }

  @VisibleForTesting
  Status getError() {
    return error;
  }
}
