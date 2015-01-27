/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.transport.okhttp;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.ThreadFactoryBuilder;

import io.grpc.AbstractChannelBuilder;
import io.grpc.SharedResourceHolder;
import io.grpc.SharedResourceHolder.Resource;
import io.grpc.transport.ClientTransportFactory;

import java.net.InetSocketAddress;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import javax.net.ssl.SSLSocketFactory;

/** Convenience class for building channels with the OkHttp transport. */
public final class OkHttpChannelBuilder extends AbstractChannelBuilder<OkHttpChannelBuilder> {
  private static final Resource<ExecutorService> DEFAULT_TRANSPORT_THREAD_POOL
      = new Resource<ExecutorService>() {
        @Override
        public ExecutorService create() {
          return Executors.newCachedThreadPool(new ThreadFactoryBuilder()
              .setNameFormat("grpc-okhttp-%d")
              .build());
        }

        @Override
        public void close(ExecutorService executor) {
          executor.shutdown();
        }
      };

  /** Creates a new builder for the given server host and port. */
  public static OkHttpChannelBuilder forAddress(String host, int port) {
    return new OkHttpChannelBuilder(new InetSocketAddress(host, port), host);
  }

  private final InetSocketAddress serverAddress;
  private ExecutorService transportExecutor;
  private String host;
  private SSLSocketFactory sslSocketFactory;

  private OkHttpChannelBuilder(InetSocketAddress serverAddress, String host) {
    this.serverAddress = Preconditions.checkNotNull(serverAddress, "serverAddress");
    this.host = host;
  }

  /**
   * Override the default executor necessary for internal transport use.
   *
   * <p>The channel does not take ownership of the given executor. It is the caller' responsibility
   * to shutdown the executor when appropriate.
   */
  public OkHttpChannelBuilder transportExecutor(ExecutorService executor) {
    this.transportExecutor = executor;
    return this;
  }

  /**
   * Overrides the host used with TLS and HTTP virtual hosting. It does not change what host is
   * actually connected to.
   *
   * <p>Should only used by tests.
   */
  public void overrideHostForAuthority(String host) {
    this.host = host;
  }

  /**
   * Provides a SSLSocketFactory to establish a secure connection. By default TLS is not enabled.
   */
  public OkHttpChannelBuilder sslSocketFactory(SSLSocketFactory factory) {
    this.sslSocketFactory = factory;
    return this;
  }

  @Override
  protected ChannelEssentials buildEssentials() {
    final ExecutorService executor = (transportExecutor == null)
        ? SharedResourceHolder.get(DEFAULT_TRANSPORT_THREAD_POOL) : transportExecutor;
    ClientTransportFactory transportFactory
        = new OkHttpClientTransportFactory(serverAddress, host, executor, sslSocketFactory);
    Runnable terminationRunnable = null;
    // We shut down the executor only if we created it.
    if (transportExecutor == null) {
      terminationRunnable = new Runnable() {
        @Override
        public void run() {
          SharedResourceHolder.release(DEFAULT_TRANSPORT_THREAD_POOL, executor);
        }
      };
    }
    return new ChannelEssentials(transportFactory, terminationRunnable);
  }
}
