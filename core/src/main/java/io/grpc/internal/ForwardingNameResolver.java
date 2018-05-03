/*
 * Copyright 2017 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.base.MoreObjects;
import io.grpc.NameResolver;

/**
* A forwarding class to ensure non overridden methods are forwarded to the delegate.
 */
abstract class ForwardingNameResolver extends NameResolver {
  private final NameResolver delegate;

  ForwardingNameResolver(NameResolver delegate) {
    checkNotNull(delegate, "delegate can not be null");
    this.delegate = delegate;
  }

  @Override
  public String getServiceAuthority() {
    return delegate.getServiceAuthority();
  }

  @Override
  public void start(Listener listener) {
    delegate.start(listener);
  }

  @Override
  public void shutdown() {
    delegate.shutdown();
  }

  @Override
  public void refresh() {
    delegate.refresh();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate).toString();
  }
}
