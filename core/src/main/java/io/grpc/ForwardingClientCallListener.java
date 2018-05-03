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
 * A {@link ClientCall.Listener} which forwards all of its methods to another {@link
 * ClientCall.Listener}.
 */
public abstract class ForwardingClientCallListener<RespT>
    extends PartialForwardingClientCallListener<RespT> {
  /**
   * Returns the delegated {@code ClientCall.Listener}.
   */
  @Override
  protected abstract ClientCall.Listener<RespT> delegate();

  @Override
  public void onMessage(RespT message) {
    delegate().onMessage(message);
  }

  /**
   * A simplified version of {@link ForwardingClientCallListener} where subclasses can pass in a
   * {@link ClientCall.Listener} as the delegate.
   */
  public abstract static class SimpleForwardingClientCallListener<RespT>
      extends ForwardingClientCallListener<RespT> {

    private final ClientCall.Listener<RespT> delegate;

    protected SimpleForwardingClientCallListener(ClientCall.Listener<RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    protected ClientCall.Listener<RespT> delegate() {
      return delegate;
    }
  }
}
