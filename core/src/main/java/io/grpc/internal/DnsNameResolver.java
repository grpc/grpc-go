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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import io.grpc.Attributes;
import io.grpc.EquivalentAddressGroup;
import io.grpc.NameResolver;
import io.grpc.Status;
import io.grpc.internal.SharedResourceHolder.Resource;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.TimeUnit;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;
import javax.naming.NamingEnumeration;
import javax.naming.NamingException;
import javax.naming.directory.Attribute;
import javax.naming.directory.InitialDirContext;

/**
 * A DNS-based {@link NameResolver}.
 *
 * <p>Each {@code A} or {@code AAAA} record emits an {@link EquivalentAddressGroup} in the list
 * passed to {@link NameResolver.Listener#onUpdate}
 *
 * @see DnsNameResolverProvider
 */
final class DnsNameResolver extends NameResolver {

  private static final Logger logger = Logger.getLogger(DnsNameResolver.class.getName());

  private static final boolean isJndiAvailable = jndiAvailable();

  @VisibleForTesting
  static boolean enableJndi = false;

  private DelegateResolver delegateResolver = pickDelegateResolver();

  private final String authority;
  private final String host;
  private final int port;
  private final Resource<ScheduledExecutorService> timerServiceResource;
  private final Resource<ExecutorService> executorResource;
  @GuardedBy("this")
  private boolean shutdown;
  @GuardedBy("this")
  private ScheduledExecutorService timerService;
  @GuardedBy("this")
  private ExecutorService executor;
  @GuardedBy("this")
  private ScheduledFuture<?> resolutionTask;
  @GuardedBy("this")
  private boolean resolving;
  @GuardedBy("this")
  private Listener listener;

  DnsNameResolver(@Nullable String nsAuthority, String name, Attributes params,
      Resource<ScheduledExecutorService> timerServiceResource,
      Resource<ExecutorService> executorResource) {
    // TODO: if a DNS server is provided as nsAuthority, use it.
    // https://www.captechconsulting.com/blogs/accessing-the-dusty-corners-of-dns-with-java
    this.timerServiceResource = timerServiceResource;
    this.executorResource = executorResource;
    // Must prepend a "//" to the name when constructing a URI, otherwise it will be treated as an
    // opaque URI, thus the authority and host of the resulted URI would be null.
    URI nameUri = URI.create("//" + name);
    authority = Preconditions.checkNotNull(nameUri.getAuthority(),
        "nameUri (%s) doesn't have an authority", nameUri);
    host = Preconditions.checkNotNull(nameUri.getHost(), "host");
    if (nameUri.getPort() == -1) {
      Integer defaultPort = params.get(NameResolver.Factory.PARAMS_DEFAULT_PORT);
      if (defaultPort != null) {
        port = defaultPort;
      } else {
        throw new IllegalArgumentException(
            "name '" + name + "' doesn't contain a port, and default port is not set in params");
      }
    } else {
      port = nameUri.getPort();
    }
  }

  @Override
  public final String getServiceAuthority() {
    return authority;
  }

  @Override
  public final synchronized void start(Listener listener) {
    Preconditions.checkState(this.listener == null, "already started");
    timerService = SharedResourceHolder.get(timerServiceResource);
    executor = SharedResourceHolder.get(executorResource);
    this.listener = Preconditions.checkNotNull(listener, "listener");
    resolve();
  }

  @Override
  public final synchronized void refresh() {
    Preconditions.checkState(listener != null, "not started");
    resolve();
  }

  private final Runnable resolutionRunnable = new Runnable() {
      @Override
      public void run() {
        Listener savedListener;
        synchronized (DnsNameResolver.this) {
          // If this task is started by refresh(), there might already be a scheduled task.
          if (resolutionTask != null) {
            resolutionTask.cancel(false);
            resolutionTask = null;
          }
          if (shutdown) {
            return;
          }
          savedListener = listener;
          resolving = true;
        }
        try {
          if (System.getenv("GRPC_PROXY_EXP") != null) {
            EquivalentAddressGroup server =
                new EquivalentAddressGroup(InetSocketAddress.createUnresolved(host, port));
            savedListener.onAddresses(Collections.singletonList(server), Attributes.EMPTY);
            return;
          }
          ResolutionResults resolvedInetAddrs;
          try {
            resolvedInetAddrs = delegateResolver.resolve(host);
          } catch (Exception e) {
            synchronized (DnsNameResolver.this) {
              if (shutdown) {
                return;
              }
              // Because timerService is the single-threaded GrpcUtil.TIMER_SERVICE in production,
              // we need to delegate the blocking work to the executor
              resolutionTask =
                  timerService.schedule(new LogExceptionRunnable(resolutionRunnableOnExecutor),
                      1, TimeUnit.MINUTES);
            }
            savedListener.onError(Status.UNAVAILABLE.withCause(e));
            return;
          }
          // Each address forms an EAG
          ArrayList<EquivalentAddressGroup> servers = new ArrayList<EquivalentAddressGroup>();
          for (InetAddress inetAddr : resolvedInetAddrs.addresses) {
            servers.add(new EquivalentAddressGroup(new InetSocketAddress(inetAddr, port)));
          }
          savedListener.onAddresses(servers, Attributes.EMPTY);
        } finally {
          synchronized (DnsNameResolver.this) {
            resolving = false;
          }
        }
      }
    };

  private final Runnable resolutionRunnableOnExecutor = new Runnable() {
      @Override
      public void run() {
        synchronized (DnsNameResolver.this) {
          if (!shutdown) {
            executor.execute(resolutionRunnable);
          }
        }
      }
    };

  @GuardedBy("this")
  private void resolve() {
    if (resolving || shutdown) {
      return;
    }
    executor.execute(resolutionRunnable);
  }

  @Override
  public final synchronized void shutdown() {
    if (shutdown) {
      return;
    }
    shutdown = true;
    if (resolutionTask != null) {
      resolutionTask.cancel(false);
    }
    if (timerService != null) {
      timerService = SharedResourceHolder.release(timerServiceResource, timerService);
    }
    if (executor != null) {
      executor = SharedResourceHolder.release(executorResource, executor);
    }
  }

  final int getPort() {
    return port;
  }

  private DelegateResolver pickDelegateResolver() {
    JdkResolver jdkResolver = new JdkResolver();
    if (isJndiAvailable && enableJndi) {
      return new CompositeResolver(jdkResolver, new JndiResolver());
    }
    return jdkResolver;
  }

  /**
   * Forces the resolver.  This should only be used by testing code.
   */
  @VisibleForTesting
  void setDelegateResolver(DelegateResolver delegateResolver) {
    this.delegateResolver = delegateResolver;
  }

  /**
   * Returns whether the JNDI DNS resolver is available.  This is accomplished by looking up a
   * particular class.  It is believed to be the default (only?) DNS resolver that will actually be
   * used.  It is provided by the OpenJDK, but unlikely Android.  Actual resolution will be done by
   * using a service provider when a hostname query is present, so the {@code DnsContextFactory}
   * may not actually be used to perform the query.  This is believed to be "okay."
   */
  @VisibleForTesting
  @SuppressWarnings("LiteralClassName")
  static boolean jndiAvailable() {
    try {
      Class.forName("javax.naming.directory.InitialDirContext");
      Class.forName("com.sun.jndi.dns.DnsContextFactory");
    } catch (ClassNotFoundException e) {
      logger.log(Level.FINE, "Unable to find JNDI DNS resolver, skipping", e);
      return false;
    }
    return true;
  }

  /**
   * Common interface between the delegate resolvers used by DnsNameResolver.
   */
  @VisibleForTesting
  abstract static class DelegateResolver {
    abstract ResolutionResults resolve(String host) throws Exception;
  }

  /**
   * Describes the results from a DNS query.
   */
  @VisibleForTesting
  static final class ResolutionResults {
    final List<InetAddress> addresses;
    final List<String> txtRecords;

    ResolutionResults(List<InetAddress> addresses, List<String> txtRecords) {
      this.addresses = Collections.unmodifiableList(checkNotNull(addresses, "addresses"));
      this.txtRecords = Collections.unmodifiableList(checkNotNull(txtRecords, "txtRecords"));
    }
  }

  /**
   * A composite DNS resolver that uses both the JDK and JNDI resolvers as delegate.  It is
   * expected that two DNS queries will be executed, with the second one being from JNDI.
   */
  @VisibleForTesting
  static final class CompositeResolver extends DelegateResolver {

    private final DelegateResolver jdkResovler;
    private final DelegateResolver jndiResovler;

    CompositeResolver(DelegateResolver jdkResovler, DelegateResolver jndiResovler) {
      this.jdkResovler = jdkResovler;
      this.jndiResovler = jndiResovler;
    }

    @Override
    ResolutionResults resolve(String host) throws Exception {
      ResolutionResults jdkResults = jdkResovler.resolve(host);
      List<InetAddress> addresses = jdkResults.addresses;
      List<String> txtRecords = Collections.emptyList();
      try {
        ResolutionResults jdniResults = jndiResovler.resolve(host);
        txtRecords = jdniResults.txtRecords;
      } catch (Exception e) {
        logger.log(Level.SEVERE, "Failed to resolve TXT results", e);
      }

      return new ResolutionResults(addresses, txtRecords);
    }
  }

  /**
   * The default name resolver provided with the JDK.  This is unable to lookup TXT records, but
   * provides address ordering sorted according to RFC 3484.  This is true on OpenJDK, because it
   * in turn calls into libc which sorts addresses in order of reachability.
   */
  @VisibleForTesting
  static final class JdkResolver extends DelegateResolver {

    @Override
    ResolutionResults resolve(String host) throws Exception {
      return new ResolutionResults(
          Arrays.asList(InetAddress.getAllByName(host)),
          Collections.<String>emptyList());
    }
  }

  /**
   * A resolver that uses JNDI.  This class is capable of looking up both addresses
   * and text records, but does not provide ordering guarantees.  It is currently not used for
   * address resolution.
   */
  @VisibleForTesting
  static final class JndiResolver extends DelegateResolver {

    private static final String[] rrTypes = new String[]{"TXT"};

    @Override
    ResolutionResults resolve(String host) throws NamingException {

      InitialDirContext dirContext = new InitialDirContext();
      javax.naming.directory.Attributes attrs = dirContext.getAttributes("dns:///" + host, rrTypes);
      List<InetAddress> addresses = new ArrayList<InetAddress>();
      List<String> txtRecords = new ArrayList<String>();

      NamingEnumeration<? extends Attribute> rrGroups = attrs.getAll();
      try {
        while (rrGroups.hasMore()) {
          Attribute rrEntry = rrGroups.next();
          assert Arrays.asList(rrTypes).contains(rrEntry.getID());
          NamingEnumeration<?> rrValues = rrEntry.getAll();
          try {
            while (rrValues.hasMore()) {
              String rrValue = (String) rrValues.next();
              txtRecords.add(rrValue);
            }
          } finally {
            rrValues.close();
          }
        }
      } finally {
        rrGroups.close();
      }

      return new ResolutionResults(addresses, txtRecords);
    }
  }
}
