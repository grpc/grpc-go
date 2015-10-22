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

import com.google.common.base.Preconditions;

import io.grpc.internal.GrpcUtil;
import io.grpc.internal.SharedResourceHolder;

import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URI;
import java.util.ArrayList;
import java.util.concurrent.ExecutorService;

import javax.annotation.Nullable;

/**
 * A factory for DNS-based {@link NameResolver}s.
 *
 * <p>It resolves a target URI whose scheme is {@code "dns"}. The (optional) authority of the target
 * URI is reserved for the address of alternative DNS server (not implemented yet). The path of the
 * target URI, exluding the leading slash {@code '/'}, is treated as the host name to be resolved by
 * DNS. Example target URIs:
 * <ul>
 *   <li>{@code "dns:///foo.googleapis.com:8080"} (using default DNS)</li>
 *   <li>{@code "dns://8.8.8.8/foo.googleapis.com:8080"} (using alternative DNS (not implemented
 *   yet))</li>
 *   <li>{@code "dns:///foo.googleapis.com"} (without port)</li>
 * </ul>
 */
@ExperimentalApi
public final class DnsNameResolverFactory extends NameResolver.Factory {

  private static final DnsNameResolverFactory instance = new DnsNameResolverFactory();

  @Override
  public NameResolver newNameResolver(URI targetUri) {
    if ("dns".equals(targetUri.getScheme())) {
      String targetPath = Preconditions.checkNotNull(targetUri.getPath(), "targetPath");
      Preconditions.checkArgument(targetPath.startsWith("/"),
          "the path component (%s) of the target (%s) must start with '/'", targetPath, targetUri);
      String name = targetPath.substring(1);
      return new DnsNameResolver(targetUri.getAuthority(), name);
    } else {
      return null;
    }
  }

  private DnsNameResolverFactory() {
  }

  public static DnsNameResolverFactory getInstance() {
    return instance;
  }

  private static class DnsNameResolver extends NameResolver {
    private final String authority;
    private final String host;
    private final int port;
    private ExecutorService executor;

    DnsNameResolver(@Nullable String nsAuthority, String name) {
      // TODO: if a DNS server is provided as nsAuthority, use it.
      // https://www.captechconsulting.com/blogs/accessing-the-dusty-corners-of-dns-with-java

      // Must prepend a "//" to the name when constructing a URI, otherwise
      // the authority and host of the resulted URI would be null.
      URI nameUri = URI.create("//" + name);
      authority = Preconditions.checkNotNull(nameUri.getAuthority(),
          "nameUri (%s) doesn't have an authority", nameUri);
      host = Preconditions.checkNotNull(nameUri.getHost(), "host");
      port = nameUri.getPort();
      Preconditions.checkArgument(port > 0, "port (%s) must be positive", port);
    }

    @Override
    public String getServiceAuthority() {
      return authority;
    }

    @Override
    public synchronized void start(final Listener listener) {
      Preconditions.checkState(executor == null, "already started");
      executor = SharedResourceHolder.get(GrpcUtil.SHARED_CHANNEL_EXECUTOR);
      executor.execute(new Runnable() {
        @Override
        public void run() {
          InetAddress[] inetAddrs;
          try {
            inetAddrs = InetAddress.getAllByName(host);
          } catch (Exception e) {
            listener.onError(Status.UNAVAILABLE.withCause(e));
            return;
          }
          ArrayList<ResolvedServerInfo> servers
              = new ArrayList<ResolvedServerInfo>(inetAddrs.length);
          for (int i = 0; i < inetAddrs.length; i++) {
            InetAddress inetAddr = inetAddrs[i];
            servers.add(
                new ResolvedServerInfo(new InetSocketAddress(inetAddr, port), Attributes.EMPTY));
          }
          listener.onUpdate(servers, Attributes.EMPTY);
        }
      });
    }

    @Override
    public synchronized void shutdown() {
      if (executor != null) {
        executor = SharedResourceHolder.release(GrpcUtil.SHARED_CHANNEL_EXECUTOR, executor);
      }
    }
  }
}
