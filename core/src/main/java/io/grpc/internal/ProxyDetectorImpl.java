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

package io.grpc.internal;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Supplier;
import io.grpc.HttpConnectProxiedSocketAddress;
import io.grpc.ProxiedSocketAddress;
import io.grpc.ProxyDetector;
import java.io.IOException;
import java.net.Authenticator;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.MalformedURLException;
import java.net.PasswordAuthentication;
import java.net.Proxy;
import java.net.ProxySelector;
import java.net.SocketAddress;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.URL;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * A utility class that detects proxies using {@link ProxySelector} and detects authentication
 * credentials using {@link Authenticator}.
 *
 */
class ProxyDetectorImpl implements ProxyDetector {
  // To validate this code: set up a local squid proxy instance, and
  // try to communicate with grpc-test.sandbox.googleapis.com:443.
  // The endpoint runs an instance of TestServiceGrpc, see
  // AbstractInteropTest for an example how to run a
  // TestService.EmptyCall RPC.
  //
  // The instructions below assume Squid 3.5.23 and a recent
  // version of Debian.
  //
  // Set the contents of /etc/squid/squid.conf to be:
  // WARNING: THESE CONFIGS HAVE NOT BEEN REVIEWED FOR SECURITY, DO
  // NOT USE OUTSIDE OF TESTING. COMMENT OUT THIS WARNING TO
  // UNBREAK THE CONFIG FILE.
  // acl SSL_ports port 443
  // acl Safe_ports port 80
  // acl Safe_ports port 21
  // acl Safe_ports port 443
  // acl Safe_ports port 70
  // acl Safe_ports port 210
  // acl Safe_ports port 1025-65535
  // acl Safe_ports port 280
  // acl Safe_ports port 488
  // acl Safe_ports port 591
  // acl Safe_ports port 777
  // acl CONNECT method CONNECT
  // http_access deny !Safe_ports
  // http_access deny CONNECT !SSL_ports
  // http_access allow localhost manager
  // http_access deny manager
  // http_access allow localhost
  // http_access deny all
  // http_port 3128
  // coredump_dir /var/spool/squid
  // refresh_pattern ^ftp: 1440 20% 10080
  // refresh_pattern ^gopher: 1440 0% 1440
  // refresh_pattern -i (/cgi-bin/|\?) 0 0% 0
  // refresh_pattern . 0 20% 4320
  //
  // Restart squid:
  // $ sudo /etc/init.d/squid restart
  //
  // To test with passwords:
  //
  // Run this command and follow the instructions to set up a user/pass:
  // $ sudo htpasswd -c /etc/squid/passwd myuser1
  //
  // Make the file readable to squid:
  // $ sudo chmod 644 /etc/squid/passwd
  //
  // Validate the username and password, you should see OK printed:
  // $ /usr/lib/squid3/basic_ncsa_auth /etc/squid/passwd
  // myuser1 <your password here>
  //
  // Add these additional lines to the beginning of squid.conf (the ordering matters):
  // auth_param basic program /usr/lib/squid3/basic_ncsa_auth /etc/squid/passwd
  // auth_param basic children 5
  // auth_param basic realm Squid proxy-caching web server
  // auth_param basic credentialsttl 2 hours
  // acl ncsa_users proxy_auth REQUIRED
  // http_access allow ncsa_users
  //
  // Restart squid:
  // $ sudo /etc/init.d/squid restart
  //
  // In both cases, start the JVM with -Dhttps.proxyHost=127.0.0.1 -Dhttps.proxyPort=3128 to
  // configure the proxy. For passwords, use java.net.Authenticator.setDefault().
  //
  // Testing with curl, no password:
  // $ curl -U myuser1:pass1 -x http://localhost:3128 -L grpc.io
  // Testing with curl, with password:
  // $ curl -U myuser1:pass1 -x http://localhost:3128 -L grpc.io
  //
  // It may be helpful to monitor the squid access logs:
  // $ sudo tail -f /var/log/squid/access.log

  private static final Logger log = Logger.getLogger(ProxyDetectorImpl.class.getName());
  private static final AuthenticationProvider DEFAULT_AUTHENTICATOR = new AuthenticationProvider() {
    @Override
    public PasswordAuthentication requestPasswordAuthentication(
        String host, InetAddress addr, int port, String protocol, String prompt, String scheme) {
      URL url = null;
      try {
        url = new URL(protocol, host, port, "");
      } catch (MalformedURLException e) {
        // let url be null
        log.log(
            Level.WARNING,
            String.format("failed to create URL for Authenticator: %s %s", protocol, host));
      }
      // TODO(spencerfang): consider using java.security.AccessController here
      return Authenticator.requestPasswordAuthentication(
          host, addr, port, protocol, prompt, scheme, url, Authenticator.RequestorType.PROXY);
    }
  };
  private static final Supplier<ProxySelector> DEFAULT_PROXY_SELECTOR =
      new Supplier<ProxySelector>() {
        @Override
        public ProxySelector get() {
          // TODO(spencerfang): consider using java.security.AccessController here
          return ProxySelector.getDefault();
        }
      };

  /**
   * @deprecated Use the standard Java proxy configuration instead with flags such as:
   *     -Dhttps.proxyHost=HOST -Dhttps.proxyPort=PORT
   */
  @Deprecated
  private static final String GRPC_PROXY_ENV_VAR = "GRPC_PROXY_EXP";
  // Do not hard code a ProxySelector because the global default ProxySelector can change
  private final Supplier<ProxySelector> proxySelector;
  private final AuthenticationProvider authenticationProvider;
  private final InetSocketAddress overrideProxyAddress;

  // We want an HTTPS proxy, which operates on the entire data stream (See IETF rfc2817).
  static final String PROXY_SCHEME = "https";

  /**
   * A proxy selector that uses the global {@link ProxySelector#getDefault()} and
   * {@link ProxyDetectorImpl.AuthenticationProvider} to detect proxy parameters.
   */
  public ProxyDetectorImpl() {
    this(DEFAULT_PROXY_SELECTOR, DEFAULT_AUTHENTICATOR, System.getenv(GRPC_PROXY_ENV_VAR));
  }

  @VisibleForTesting
  ProxyDetectorImpl(
      Supplier<ProxySelector> proxySelector,
      AuthenticationProvider authenticationProvider,
      @Nullable String proxyEnvString) {
    this.proxySelector = checkNotNull(proxySelector);
    this.authenticationProvider = checkNotNull(authenticationProvider);
    if (proxyEnvString != null) {
      overrideProxyAddress = overrideProxy(proxyEnvString);
    } else {
      overrideProxyAddress = null;
    }
  }

  @Nullable
  @Override
  public ProxiedSocketAddress proxyFor(SocketAddress targetServerAddress) throws IOException {
    if (!(targetServerAddress instanceof InetSocketAddress)) {
      return null;
    }
    if (overrideProxyAddress != null) {
      return HttpConnectProxiedSocketAddress.newBuilder()
          .setProxyAddress(overrideProxyAddress)
          .setTargetAddress((InetSocketAddress) targetServerAddress)
          .build();
    }
    return detectProxy((InetSocketAddress) targetServerAddress);
  }

  private ProxiedSocketAddress detectProxy(InetSocketAddress targetAddr) throws IOException {
    URI uri;
    String host;
    try {
      host = GrpcUtil.getHost(targetAddr);
    } catch (Throwable t) {
      // Workaround for Android API levels < 19 if getHostName causes a NetworkOnMainThreadException
      log.log(Level.WARNING, "Failed to get host for proxy lookup, proceeding without proxy", t);
      return null;
    }
    try {
      uri =
          new URI(
              PROXY_SCHEME,
              null, /* userInfo */
              host,
              targetAddr.getPort(),
              null, /* path */
              null, /* query */
              null /* fragment */);
    } catch (final URISyntaxException e) {
      log.log(
          Level.WARNING,
          "Failed to construct URI for proxy lookup, proceeding without proxy",
          e);
      return null;
    }

    ProxySelector proxySelector = this.proxySelector.get();
    if (proxySelector == null) {
      log.log(Level.FINE, "proxy selector is null, so continuing without proxy lookup");
      return null;
    }

    List<Proxy> proxies = proxySelector.select(uri);
    if (proxies.size() > 1) {
      log.warning("More than 1 proxy detected, gRPC will select the first one");
    }
    Proxy proxy = proxies.get(0);

    if (proxy.type() == Proxy.Type.DIRECT) {
      return null;
    }
    InetSocketAddress proxyAddr = (InetSocketAddress) proxy.address();
    // The prompt string should be the realm as returned by the server.
    // We don't have it because we are avoiding the full handshake.
    String promptString = "";
    PasswordAuthentication auth = authenticationProvider.requestPasswordAuthentication(
        GrpcUtil.getHost(proxyAddr),
        proxyAddr.getAddress(),
        proxyAddr.getPort(),
        PROXY_SCHEME,
        promptString,
        null);

    final InetSocketAddress resolvedProxyAddr;
    if (proxyAddr.isUnresolved()) {
      InetAddress resolvedAddress = InetAddress.getByName(proxyAddr.getHostName());
      resolvedProxyAddr = new InetSocketAddress(resolvedAddress, proxyAddr.getPort());
    } else {
      resolvedProxyAddr = proxyAddr;
    }

    HttpConnectProxiedSocketAddress.Builder builder =
        HttpConnectProxiedSocketAddress.newBuilder()
        .setTargetAddress(targetAddr)
        .setProxyAddress(resolvedProxyAddr);

    if (auth == null) {
      return builder.build();
    }

    return builder
        .setUsername(auth.getUserName())
        .setPassword(auth.getPassword() == null ? null : new String(auth.getPassword()))
        .build();
  }

  /**
   * GRPC_PROXY_EXP is deprecated but let's maintain compatibility for now.
   */
  private static InetSocketAddress overrideProxy(String proxyHostPort) {
    if (proxyHostPort == null) {
      return null;
    }

    String[] parts = proxyHostPort.split(":", 2);
    int port = 80;
    if (parts.length > 1) {
      port = Integer.parseInt(parts[1]);
    }
    log.warning(
        "Detected GRPC_PROXY_EXP and will honor it, but this feature will "
            + "be removed in a future release. Use the JVM flags "
            + "\"-Dhttps.proxyHost=HOST -Dhttps.proxyPort=PORT\" to set the https proxy for "
            + "this JVM.");
    return new InetSocketAddress(parts[0], port);
  }

  /**
   * This interface makes unit testing easier by avoiding direct calls to static methods.
   */
  interface AuthenticationProvider {
    PasswordAuthentication requestPasswordAuthentication(
        String host,
        InetAddress addr,
        int port,
        String protocol,
        String prompt,
        String scheme);
  }
}
