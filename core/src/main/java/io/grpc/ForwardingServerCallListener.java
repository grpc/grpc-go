/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc;

/**
 * A {@link ServerCall.Listener} which forwards all of its methods to another {@link
 * ServerCall.Listener} of matching parameterized types.
 */
public abstract class ForwardingServerCallListener<ReqT>
    extends PartialForwardingServerCallListener<ReqT> {
  /**
   * Returns the delegated {@code ServerCall.Listener}.
   */
  @Override
  protected abstract ServerCall.Listener<ReqT> delegate();

  @Override
  public void onMessage(ReqT message) {
    delegate().onMessage(message);
  }

  /**
   * A simplified version of {@link ForwardingServerCallListener} where subclasses can pass in a
   * {@link ServerCall.Listener} as the delegate.
   */
  public abstract static class SimpleForwardingServerCallListener<ReqT>
      extends ForwardingServerCallListener<ReqT> {

    private final ServerCall.Listener<ReqT> delegate;

    protected SimpleForwardingServerCallListener(ServerCall.Listener<ReqT> delegate) {
      this.delegate = delegate;
    }

    @Override
    protected ServerCall.Listener<ReqT> delegate() {
      return delegate;
    }
  }
}
