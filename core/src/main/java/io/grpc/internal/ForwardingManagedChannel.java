/*
 * Copyright 2018 The gRPC Authors
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
import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ConnectivityState;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import java.util.concurrent.TimeUnit;

abstract class ForwardingManagedChannel extends ManagedChannel {

  private final ManagedChannel delegate;

  ForwardingManagedChannel(ManagedChannel delegate) {
    this.delegate = delegate;
  }

  @Override
  public ManagedChannel shutdown() {
    return delegate.shutdown();
  }

  @Override
  public boolean isShutdown() {
    return delegate.isShutdown();
  }

  @Override
  public boolean isTerminated() {
    return delegate.isTerminated();
  }

  @Override
  public ManagedChannel shutdownNow() {
    return delegate.shutdownNow();
  }

  @Override
  public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
    return delegate.awaitTermination(timeout, unit);
  }

  @Override
  public <RequestT, ResponseT> ClientCall<RequestT, ResponseT> newCall(
      MethodDescriptor<RequestT, ResponseT> methodDescriptor, CallOptions callOptions) {
    return delegate.newCall(methodDescriptor, callOptions);
  }

  @Override
  public String authority() {
    return delegate.authority();
  }

  @Override
  public ConnectivityState getState(boolean requestConnection) {
    return delegate.getState(requestConnection);
  }

  @Override
  public void notifyWhenStateChanged(ConnectivityState source, Runnable callback) {
    delegate.notifyWhenStateChanged(source, callback);
  }

  @Override
  public void resetConnectBackoff() {
    delegate.resetConnectBackoff();
  }

  @Override
  public void enterIdle() {
    delegate.enterIdle();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate).toString();
  }
}
