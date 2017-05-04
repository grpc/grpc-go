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

package io.grpc.inprocess;

import com.google.common.base.Preconditions;
import io.grpc.ExperimentalApi;
import io.grpc.Internal;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.ConnectionClientTransport;
import java.net.SocketAddress;

/**
 * Builder for a channel that issues in-process requests. Clients identify the in-process server by
 * its name.
 *
 * <p>The channel is intended to be fully-featured, high performance, and useful in testing.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1783")
public class InProcessChannelBuilder extends
        AbstractManagedChannelImplBuilder<InProcessChannelBuilder> {
  /**
   * Create a channel builder that will connect to the server with the given name.
   *
   * @param name the identity of the server to connect to
   * @return a new builder
   */
  public static InProcessChannelBuilder forName(String name) {
    return new InProcessChannelBuilder(name);
  }

  private final String name;

  private InProcessChannelBuilder(String name) {
    super(new InProcessSocketAddress(name), "localhost");
    this.name = Preconditions.checkNotNull(name, "name");
  }

  @Override
  public final InProcessChannelBuilder maxInboundMessageSize(int max) {
    // TODO(carl-mastrangelo): maybe throw an exception since this not enforced?
    return super.maxInboundMessageSize(max);
  }

  /**
   * Does nothing.
   */
  @Override
  public InProcessChannelBuilder usePlaintext(boolean skipNegotiation) {
    return this;
  }

  @Override
  @Internal
  protected ClientTransportFactory buildTransportFactory() {
    return new InProcessClientTransportFactory(name);
  }

  @Override
  @Internal
  protected boolean recordsStats() {
    // TODO(zhangkun83): InProcessTransport by-passes framer and deframer, thus message sizses are
    // not counted.  Therefore, we disable stats for now.
    // (https://github.com/grpc/grpc-java/issues/2284)
    return false;
  }

  /**
   * Creates InProcess transports. Exposed for internal use, as it should be private.
   */
  @Internal
  static final class InProcessClientTransportFactory implements ClientTransportFactory {
    private final String name;

    private boolean closed;

    private InProcessClientTransportFactory(String name) {
      this.name = name;
    }

    @Override
    public ConnectionClientTransport newClientTransport(
        SocketAddress addr, String authority, String userAgent) {
      if (closed) {
        throw new IllegalStateException("The transport factory is closed.");
      }
      return new InProcessTransport(name, authority);
    }

    @Override
    public void close() {
      closed = true;
      // Do nothing.
    }
  }
}
