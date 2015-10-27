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

package io.grpc.internal;

import static io.grpc.internal.GrpcUtil.TIMER_SERVICE;

import com.google.common.base.Preconditions;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;

import io.grpc.Attributes;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors;
import io.grpc.Codec;
import io.grpc.Compressor;
import io.grpc.DecompressorRegistry;
import io.grpc.ExperimentalApi;
import io.grpc.LoadBalancer;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.NameResolver;
import io.grpc.ResolvedServerInfo;
import io.grpc.Status;
import io.grpc.TransportManager;
import io.grpc.internal.ClientCallImpl.ClientTransportProvider;

import java.net.SocketAddress;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.logging.Logger;

import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.annotation.concurrent.ThreadSafe;

/** A communication channel for making outgoing RPCs. */
@ThreadSafe
public final class ManagedChannelImpl extends ManagedChannel {
  private static final Logger log = Logger.getLogger(ManagedChannelImpl.class.getName());

  private static final ListenableFuture<ClientTransport> NULL_VALUE_TRANSPORT_FUTURE =
        Futures.immediateFuture(null);

  private final ClientTransportFactory transportFactory;
  private final Executor executor;
  private final boolean usingSharedExecutor;
  private final String userAgent;
  private final Object lock = new Object();

  private final DecompressorRegistry decompressorRegistry =
      DecompressorRegistry.getDefaultInstance();

  /**
   * Executor that runs deadline timers for requests.
   */
  private ScheduledExecutorService scheduledExecutor;

  private final BackoffPolicy.Provider backoffPolicyProvider;

  /**
   * We delegate to this channel, so that we can have interceptors as necessary. If there aren't
   * any interceptors this will just be {@link RealChannel}.
   */
  private final Channel interceptorChannel;

  private final NameResolver nameResolver;
  private final LoadBalancer loadBalancer;

  /**
   * Maps addresses to transports for that server.
   */
  @GuardedBy("lock")
  private final Map<SocketAddress, TransportSet> transports =
      new HashMap<SocketAddress, TransportSet>();

  @GuardedBy("lock")
  private boolean shutdown;
  @GuardedBy("lock")
  private boolean terminated;

  private volatile Compressor defaultCompressor;

  private final ClientTransportProvider transportProvider = new ClientTransportProvider() {
    @Override
    public ListenableFuture<ClientTransport> get(CallOptions callOptions) {
      synchronized (lock) {
        if (shutdown) {
          return NULL_VALUE_TRANSPORT_FUTURE;
        }
      }
      return loadBalancer.pickTransport(callOptions.getRequestKey());
    }
  };

  ManagedChannelImpl(String target, BackoffPolicy.Provider backoffPolicyProvider,
      NameResolver.Factory nameResolverFactory, Attributes nameResolverParams,
      LoadBalancer.Factory loadBalancerFactory,
      ClientTransportFactory transportFactory, @Nullable Executor executor,
      @Nullable String userAgent, List<ClientInterceptor> interceptors) {
    if (executor == null) {
      usingSharedExecutor = true;
      this.executor = SharedResourceHolder.get(GrpcUtil.SHARED_CHANNEL_EXECUTOR);
    } else {
      usingSharedExecutor = false;
      this.executor = executor;
    }
    this.backoffPolicyProvider = backoffPolicyProvider;

    // Finding a NameResolver. Try using the target string as the URI. If that fails, try prepending
    // "dns:///".
    NameResolver nameResolver = null;
    URI targetUri;
    StringBuilder uriSyntaxErrors = new StringBuilder();
    try {
      targetUri = new URI(target);
      nameResolver = nameResolverFactory.newNameResolver(targetUri, nameResolverParams);
      // For "localhost:8080" this would likely return null, because "localhost" is parsed as the
      // scheme. Will fall into the next branch and try "dns:///localhost:8080".
    } catch (URISyntaxException e) {
      // "foo.googleapis.com:8080" will trigger this exception, because "foo.googleapis.com" is an
      // invalid scheme. Just fall through and will try "dns:///foo.googleapis.com:8080"
      uriSyntaxErrors.append(e.getMessage());
    }
    if (nameResolver == null) {
      try {
        targetUri = new URI("dns:///" + target);
        nameResolver = nameResolverFactory.newNameResolver(targetUri, nameResolverParams);
      } catch (URISyntaxException e) {
        if (uriSyntaxErrors.length() > 0) {
          uriSyntaxErrors.append("; ");
        }
        uriSyntaxErrors.append(e.getMessage());
      }
    }
    Preconditions.checkArgument(nameResolver != null,
        "cannot find a NameResolver for %s%s", target,
        uriSyntaxErrors.length() > 0 ? " (" + uriSyntaxErrors.toString() + ")" : "");
    this.nameResolver = nameResolver;

    this.loadBalancer = loadBalancerFactory.newLoadBalancer(nameResolver.getServiceAuthority(), tm);
    this.transportFactory = transportFactory;
    this.userAgent = userAgent;
    this.interceptorChannel = ClientInterceptors.intercept(new RealChannel(), interceptors);
    scheduledExecutor = SharedResourceHolder.get(TIMER_SERVICE);

    this.nameResolver.start(new NameResolver.Listener() {
      @Override
      public void onUpdate(List<ResolvedServerInfo> servers, Attributes config) {
        loadBalancer.handleResolvedAddresses(servers, config);
      }

      @Override
      public void onError(Status error) {
        Preconditions.checkArgument(!error.isOk(), "the error status must not be OK");
        loadBalancer.handleNameResolutionError(error);
      }
    });
  }

  /**
   * Sets the default compression method for this Channel.  By default, new calls will use the
   * provided compressor.  Each individual Call can override this by specifying it in CallOptions.
   * If the remote host does not support the message encoding, the call will likely break.  There
   * is currently no provided way to discover what message encodings the remote host supports.
   * @param c The compressor to use.  If {@code null} no compression will by performed.  This is
   *          equivalent to using {@code Codec.Identity.NONE}.  If not null, the Compressor must be
   *          threadsafe.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
  public void setDefaultCompressor(@Nullable Compressor c) {
    defaultCompressor = (c != null) ? c : Codec.Identity.NONE;
  }

  /**
   * Initiates an orderly shutdown in which preexisting calls continue but new calls are immediately
   * cancelled.
   */
  @Override
  public ManagedChannelImpl shutdown() {
    ArrayList<TransportSet> transportsCopy = new ArrayList<TransportSet>();
    synchronized (lock) {
      if (shutdown) {
        return this;
      }
      shutdown = true;
      // After shutdown there are no new calls, so no new cancellation tasks are needed
      scheduledExecutor = SharedResourceHolder.release(TIMER_SERVICE, scheduledExecutor);
      if (transports.isEmpty()) {
        terminated = true;
        lock.notifyAll();
        onChannelTerminated();
      } else {
        transportsCopy.addAll(transports.values());
      }
    }
    for (TransportSet ts : transportsCopy) {
      ts.shutdown();
    }
    return this;
  }

  /**
   * Initiates a forceful shutdown in which preexisting and new calls are cancelled. Although
   * forceful, the shutdown process is still not instantaneous; {@link #isTerminated()} will likely
   * return {@code false} immediately after this method returns.
   *
   * <p>NOT YET IMPLEMENTED. This method currently behaves identically to shutdown().
   */
  // TODO(ejona86): cancel preexisting calls.
  @Override
  public ManagedChannelImpl shutdownNow() {
    shutdown();
    return this;
  }

  @Override
  public boolean isShutdown() {
    synchronized (lock) {
      return shutdown;
    }
  }

  @Override
  public boolean awaitTermination(long timeout, TimeUnit unit) throws InterruptedException {
    synchronized (lock) {
      long timeoutNanos = unit.toNanos(timeout);
      long endTimeNanos = System.nanoTime() + timeoutNanos;
      while (!terminated && (timeoutNanos = endTimeNanos - System.nanoTime()) > 0) {
        TimeUnit.NANOSECONDS.timedWait(lock, timeoutNanos);
      }
      return terminated;
    }
  }

  @Override
  public boolean isTerminated() {
    synchronized (lock) {
      return terminated;
    }
  }

  /*
   * Creates a new outgoing call on the channel.
   */
  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
      CallOptions callOptions) {
    boolean hasCodecOverride = callOptions.getCompressor() != null;
    if (!hasCodecOverride && defaultCompressor != Codec.Identity.NONE) {
      callOptions = callOptions.withCompressor(defaultCompressor);
    }
    return interceptorChannel.newCall(method, callOptions);
  }

  @Override
  public String authority() {
    return interceptorChannel.authority();
  }

  private class RealChannel extends Channel {
    @Override
    public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method,
        CallOptions callOptions) {
      return new ClientCallImpl<ReqT, RespT>(
          method,
          new SerializingExecutor(executor),
          callOptions,
          transportProvider,
          scheduledExecutor)
              .setUserAgent(userAgent)
              .setDecompressorRegistry(decompressorRegistry);
    }

    @Override
    public String authority() {
      String authority = nameResolver.getServiceAuthority();
      return Preconditions.checkNotNull(authority, "authority");
    }
  }

  /**
   * If we're using the shared executor, returns its reference.
   */
  private void onChannelTerminated() {
    if (usingSharedExecutor) {
      SharedResourceHolder.release(GrpcUtil.SHARED_CHANNEL_EXECUTOR, (ExecutorService) executor);
    }
    // Release the transport factory so that it can deallocate any resources.
    transportFactory.release();
  }

  private final TransportManager tm = new TransportManager() {
    @Override
    public void updateRetainedTransports(SocketAddress[] addrs) {
      // TODO(zhangkun83): warm-up new servers and discard removed servers.
    }

    @Override
    public ListenableFuture<ClientTransport> getTransport(final SocketAddress addr) {
      TransportSet ts;
      synchronized (lock) {
        if (shutdown) {
          return null;
        }
        ts = transports.get(addr);
        if (ts == null) {
          ts = new TransportSet(addr, authority(), loadBalancer, backoffPolicyProvider,
              transportFactory, scheduledExecutor, new TransportSet.Callback() {
                @Override
                public void onTerminated() {
                  synchronized (lock) {
                    transports.remove(addr);
                    if (shutdown && transports.isEmpty()) {
                      if (terminated) {
                        log.warning("transportTerminated called after already terminated");
                      }
                      terminated = true;
                      lock.notifyAll();
                      onChannelTerminated();
                    }
                  }
                }
              });
          transports.put(addr, ts);
        }
      }
      return ts.obtainActiveTransport();
    }
  };
}
