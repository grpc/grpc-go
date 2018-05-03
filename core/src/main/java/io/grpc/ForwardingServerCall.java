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
 * A {@link ServerCall} which forwards all of it's methods to another {@link ServerCall}.
 */
public abstract class ForwardingServerCall<ReqT, RespT>
    extends PartialForwardingServerCall<ReqT, RespT> {
  /**
   * Returns the delegated {@code ServerCall}.
   */
  @Override
  protected abstract ServerCall<ReqT, RespT> delegate();

  @Override
  public void sendMessage(RespT message) {
    delegate().sendMessage(message);
  }

  /**
   * A simplified version of {@link ForwardingServerCall} where subclasses can pass in a {@link
   * ServerCall} as the delegate.
   */
  public abstract static class SimpleForwardingServerCall<ReqT, RespT>
      extends ForwardingServerCall<ReqT, RespT> {

    private final ServerCall<ReqT, RespT> delegate;

    protected SimpleForwardingServerCall(ServerCall<ReqT, RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    protected ServerCall<ReqT, RespT> delegate() {
      return delegate;
    }

    @Override
    public MethodDescriptor<ReqT, RespT> getMethodDescriptor() {
      return delegate.getMethodDescriptor();
    }
  }
}
