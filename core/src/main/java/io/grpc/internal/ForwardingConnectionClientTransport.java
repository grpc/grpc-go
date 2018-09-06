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

import com.google.common.base.MoreObjects;
import com.google.common.util.concurrent.ListenableFuture;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalLogId;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.util.concurrent.Executor;

abstract class ForwardingConnectionClientTransport implements ConnectionClientTransport {
  @Override
  public Runnable start(Listener listener) {
    return delegate().start(listener);
  }

  @Override
  public void shutdown(Status status) {
    delegate().shutdown(status);
  }

  @Override
  public void shutdownNow(Status status) {
    delegate().shutdownNow(status);
  }

  @Override
  public ClientStream newStream(
      MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions) {
    return delegate().newStream(method, headers, callOptions);
  }

  @Override
  public void ping(PingCallback callback, Executor executor) {
    delegate().ping(callback, executor);
  }

  @Override
  public InternalLogId getLogId() {
    return delegate().getLogId();
  }

  @Override
  public Attributes getAttributes() {
    return delegate().getAttributes();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }

  @Override
  public ListenableFuture<SocketStats> getStats() {
    return delegate().getStats();
  }

  protected abstract ConnectionClientTransport delegate();
}
