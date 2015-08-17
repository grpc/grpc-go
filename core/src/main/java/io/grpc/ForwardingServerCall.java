/*
 * Copyright 2015, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc;

/**
 * A {@link ServerCall} which forwards all of it's methods to another {@link ServerCall}.
 */
public abstract class ForwardingServerCall<RespT> extends ServerCall<RespT> {
  /**
   * Returns the delegated {@code ServerCall}.
   */
  protected abstract ServerCall<RespT> delegate();

  @Override
  public void request(int numMessages) {
    delegate().request(numMessages);
  }

  @Override
  public void sendHeaders(Metadata headers) {
    delegate().sendHeaders(headers);
  }

  @Override
  public void sendMessage(RespT message) {
    delegate().sendMessage(message);
  }

  @Override
  public boolean isReady() {
    return delegate().isReady();
  }

  @Override
  public void close(Status status, Metadata trailers) {
    delegate().close(status, trailers);
  }

  @Override
  public boolean isCancelled() {
    return delegate().isCancelled();
  }

  /**
   * A simplified version of {@link ForwardingServerCall} where subclasses can pass in a {@link
   * ServerCall} as the delegate.
   */
  public abstract static class SimpleForwardingServerCall<RespT>
      extends ForwardingServerCall<RespT> {

    private final ServerCall<RespT> delegate;

    protected SimpleForwardingServerCall(ServerCall<RespT> delegate) {
      this.delegate = delegate;
    }

    @Override
    protected ServerCall<RespT> delegate() {
      return delegate;
    }
  }
}
