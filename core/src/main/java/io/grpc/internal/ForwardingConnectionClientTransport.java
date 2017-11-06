/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import com.google.common.util.concurrent.SettableFuture;
import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import java.util.concurrent.Executor;
import java.util.concurrent.Future;

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
  public LogId getLogId() {
    return delegate().getLogId();
  }

  @Override
  public Attributes getAttributes() {
    return delegate().getAttributes();
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + "[" + delegate().toString() + "]";
  }

  @Override
  public Future<TransportTracer.Stats> getTransportStats() {
    SettableFuture<TransportTracer.Stats> ret = SettableFuture.create();
    ret.set(null);
    return ret;
  }

  protected abstract ConnectionClientTransport delegate();
}
