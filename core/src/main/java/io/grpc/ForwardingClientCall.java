/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

import javax.annotation.Nullable;

/**
 * A {@link ClientCall} which forwards all of it's methods to another {@link ClientCall}.
 */
public abstract class ForwardingClientCall<ReqT, RespT> extends ClientCall<ReqT, RespT> {
  /**
   * Returns the delegated {@code ClientCall}.
   */
  protected abstract ClientCall<ReqT, RespT> delegate();

  @Override
  public void start(Listener<RespT> responseListener, Metadata headers) {
    delegate().start(responseListener, headers);
  }

  @Override
  public void request(int numMessages) {
    delegate().request(numMessages);
  }

  @Override
  public void cancel(@Nullable String message, @Nullable Throwable cause) {
    delegate().cancel(message, cause);
  }

  @Override
  public void halfClose() {
    delegate().halfClose();
  }

  @Override
  public void sendMessage(ReqT message) {
    delegate().sendMessage(message);
  }

  @Override
  public void setMessageCompression(boolean enabled) {
    delegate().setMessageCompression(enabled);
  }

  @Override
  public boolean isReady() {
    return delegate().isReady();
  }

  @Override
  public Attributes getAttributes() {
    return delegate().getAttributes();
  }

  /**
   * A simplified version of {@link ForwardingClientCall} where subclasses can pass in a {@link
   * ClientCall} as the delegate.
   */
  public abstract static class SimpleForwardingClientCall<ReqT, RespT>
      extends ForwardingClientCall<ReqT, RespT> {
    private final ClientCall<ReqT, RespT> delegate;

    protected SimpleForwardingClientCall(ClientCall<ReqT, RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    protected ClientCall<ReqT, RespT> delegate() {
      return delegate;
    }
  }
}
