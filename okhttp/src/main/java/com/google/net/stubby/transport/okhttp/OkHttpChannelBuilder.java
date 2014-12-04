package com.google.net.stubby.transport.okhttp;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.Service;
import com.google.common.util.concurrent.ThreadFactoryBuilder;
import com.google.net.stubby.AbstractChannelBuilder;
import com.google.net.stubby.SharedResourceHolder;
import com.google.net.stubby.SharedResourceHolder.Resource;
import com.google.net.stubby.transport.ClientTransportFactory;

import java.net.InetSocketAddress;
import java.util.concurrent.Executors;
import java.util.concurrent.ExecutorService;

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
    Service.Listener listener = null;
    // We shut down the executor only if we created it.
    if (transportExecutor == null) {
      listener = new ClosureHook() {
        @Override
        protected void onClosed() {
          SharedResourceHolder.release(DEFAULT_TRANSPORT_THREAD_POOL, executor);
        }
      };
    }
    return new ChannelEssentials(transportFactory, listener);
  }
}
