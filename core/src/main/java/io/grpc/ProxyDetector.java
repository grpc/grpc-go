/*
 * Copyright 2017 The gRPC Authors
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

import java.io.IOException;
import java.net.SocketAddress;
import javax.annotation.Nullable;

/**
 * A utility class to detect which proxy, if any, should be used for a given
 * {@link java.net.SocketAddress}. This class performs network requests to resolve address names,
 * and should only be used in places that are expected to do IO such as the
 * {@link io.grpc.NameResolver}.
 *
 * <h1>How Proxies work in gRPC</h1>
 *
 * <p>In order for gRPC to use a proxy, {@link NameResolver}, {@link ProxyDetector} and the
 * underlying transport need to work together.
 *
 * <p>The {@link NameResolver} should invoke the {@link ProxyDetector} retrieved from the {@link
 * NameResolver.Helper#getProxyDetector}, and pass the returned {@link ProxiedSocketAddress} to
 * {@link NameResolver.Listener#onAddresses}.  The DNS name resolver shipped with gRPC is already
 * doing so.
 *
 * <p>The default {@code ProxyDetector} uses Java's standard {@link java.net.ProxySelector} and
 * {@link java.net.Authenticator} to detect proxies and authentication credentials and produce
 * {@link HttpConnectProxiedSocketAddress}, which is for using an HTTP CONNECT proxy.  A custom
 * {@code ProxyDetector} can be passed to {@link ManagedChannelBuilder#proxyDetector}.
 *
 * <p>The {@link ProxiedSocketAddress} is then handled by the transport.  The transport needs to
 * support whatever type of {@code ProxiedSocketAddress} returned by {@link ProxyDetector}.  The
 * Netty transport and the OkHttp transport currently only support {@link
 * HttpConnectProxiedSocketAddress} which is returned by the default {@code ProxyDetector}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/5279")
public interface ProxyDetector {
  /**
   * Given a target address, returns a proxied address if a proxy should be used. If no proxy should
   * be used, then return value will be {@code null}.
   *
   * <p>If the returned {@code ProxiedSocketAddress} contains any address that needs to be resolved
   * locally, it should be resolved before it's returned, and this method throws if unable to
   * resolve it.
   *
   * @param targetServerAddress the target address, which is generally unresolved, because the proxy
   *                            will resolve it.
   */
  @Nullable
  ProxiedSocketAddress proxyFor(SocketAddress targetServerAddress) throws IOException;
}
