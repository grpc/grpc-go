/*
 * Copyright (C) 2012 Square, Inc.
 * Copyright (C) 2012 The Android Open Source Project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Forked from OkHttp 2.5.0
 */

package io.grpc.okhttp.internal;

import io.grpc.internal.GrpcUtil;
import java.io.IOException;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.SocketException;
import java.security.AccessController;
import java.security.KeyManagementException;
import java.security.NoSuchAlgorithmException;
import java.security.PrivilegedActionException;
import java.security.PrivilegedExceptionAction;
import java.security.Provider;
import java.security.Security;
import java.util.ArrayList;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLParameters;
import javax.net.ssl.SSLSocket;
import okio.Buffer;

/**
 * Access to platform-specific features.
 *
 * <h3>Server name indication (SNI)</h3>
 *
 * Supported on Android 2.3+.
 *
 * <h3>Session Tickets</h3>
 *
 * Supported on Android 2.3+.
 *
 * <h3>Android Traffic Stats (Socket Tagging)</h3>
 *
 * Supported on Android 4.0+.
 *
 * <h3>ALPN (Application Layer Protocol Negotiation)</h3>
 *
 * Supported on Android 5.0+. The APIs were present in Android 4.4, but that implementation was
 * unstable.
 *
 * <p>Supported on OpenJDK 9+.
 *
 * <p>Supported on OpenJDK 7 and 8 (via the JettyALPN-boot library).
 */
public class Platform {
  public static final Logger logger = Logger.getLogger(Platform.class.getName());

  public enum TlsExtensionType {
    ALPN_AND_NPN,
    NPN,
    NONE,
  }

  /**
   * List of recognized security providers. The first recognized security provider according to the
   * preference order returned by {@link Security#getProviders} will be selected.
   */
  private static final String[] ANDROID_SECURITY_PROVIDERS =
      new String[] {
        // See https://developer.android.com/training/articles/security-gms-provider.html
        "com.google.android.gms.org.conscrypt.OpenSSLProvider",
        "org.conscrypt.OpenSSLProvider",
        "com.android.org.conscrypt.OpenSSLProvider",
        "org.apache.harmony.xnet.provider.jsse.OpenSSLProvider"
      };

  private static final Platform PLATFORM = findPlatform();

  public static Platform get() {
    return PLATFORM;
  }

  private final Provider sslProvider;

  public Platform(Provider sslProvider) {
    this.sslProvider = sslProvider;
  }

  /** Prefix used on custom headers. */
  public String getPrefix() {
    return "OkHttp";
  }

  public void logW(String warning) {
    System.out.println(warning);
  }

  public void tagSocket(Socket socket) throws SocketException {
  }

  public void untagSocket(Socket socket) throws SocketException {
  }

  public Provider getProvider() {
    return sslProvider;
  }

  /** Returns the TLS extension type available (ALPN and NPN, NPN, or None). */
  public TlsExtensionType getTlsExtensionType() {
    return TlsExtensionType.NONE;
  }

  /**
   * Configure TLS extensions on {@code sslSocket} for {@code route}.
   *
   * @param hostname non-null for client-side handshakes; null for
   *     server-side handshakes.
   */
  public void configureTlsExtensions(SSLSocket sslSocket, String hostname,
      List<Protocol> protocols) {
  }

  /**
   * Called after the TLS handshake to release resources allocated by {@link
   * #configureTlsExtensions}.
   */
  public void afterHandshake(SSLSocket sslSocket) {
  }

  /** Returns the negotiated protocol, or null if no protocol was negotiated. */
  public String getSelectedProtocol(SSLSocket socket) {
    return null;
  }

  public void connectSocket(Socket socket, InetSocketAddress address,
      int connectTimeout) throws IOException {
    socket.connect(address, connectTimeout);
  }

  /** Attempt to match the host runtime to a capable Platform implementation. */
  private static Platform findPlatform() {
    Provider androidOrAppEngineProvider =
        GrpcUtil.IS_RESTRICTED_APPENGINE ? getAppEngineProvider() : getAndroidSecurityProvider();
    if (androidOrAppEngineProvider != null) {
      // Attempt to find Android 2.3+ APIs.
      OptionalMethod<Socket> setUseSessionTickets
          = new OptionalMethod<Socket>(null, "setUseSessionTickets", boolean.class);
      OptionalMethod<Socket> setHostname
          = new OptionalMethod<Socket>(null, "setHostname", String.class);
      Method trafficStatsTagSocket = null;
      Method trafficStatsUntagSocket = null;
      OptionalMethod<Socket> getAlpnSelectedProtocol =
          new OptionalMethod<Socket>(byte[].class, "getAlpnSelectedProtocol");
      OptionalMethod<Socket> setAlpnProtocols =
          new OptionalMethod<Socket>(null, "setAlpnProtocols", byte[].class);

      // Attempt to find Android 4.0+ APIs.
      try {
        Class<?> trafficStats = Class.forName("android.net.TrafficStats");
        trafficStatsTagSocket = trafficStats.getMethod("tagSocket", Socket.class);
        trafficStatsUntagSocket = trafficStats.getMethod("untagSocket", Socket.class);
      } catch (ClassNotFoundException ignored) {
      } catch (NoSuchMethodException ignored) {
      }

      TlsExtensionType tlsExtensionType;
      if (GrpcUtil.IS_RESTRICTED_APPENGINE) {
        tlsExtensionType = TlsExtensionType.ALPN_AND_NPN;
      } else if (androidOrAppEngineProvider.getName().equals("GmsCore_OpenSSL")
          || androidOrAppEngineProvider.getName().equals("Conscrypt")) {
        tlsExtensionType = TlsExtensionType.ALPN_AND_NPN;
      } else if (isAtLeastAndroid5()) {
        tlsExtensionType = TlsExtensionType.ALPN_AND_NPN;
      } else if (isAtLeastAndroid41()) {
        tlsExtensionType = TlsExtensionType.NPN;
      } else {
        tlsExtensionType = TlsExtensionType.NONE;
      }
      return new Android(
          setUseSessionTickets,
          setHostname,
          trafficStatsTagSocket,
          trafficStatsUntagSocket,
          getAlpnSelectedProtocol,
          setAlpnProtocols,
          androidOrAppEngineProvider,
          tlsExtensionType);
    }
    Provider sslProvider;
    try {
      sslProvider = SSLContext.getDefault().getProvider();
    } catch (NoSuchAlgorithmException nsae) {
      throw new RuntimeException(nsae);
    }

    // Find JDK9+ ALPN support
    try {
      // getApplicationProtocol() may throw UnsupportedOperationException, so first construct a
      // dummy SSLEngine and verify the method does not throw.
      SSLContext context = SSLContext.getInstance("TLS", sslProvider);
      context.init(null, null, null);
      SSLEngine engine = context.createSSLEngine();
      Method getEngineApplicationProtocol =
          AccessController.doPrivileged(
              new PrivilegedExceptionAction<Method>() {
                @Override
                public Method run() throws Exception {
                  return SSLEngine.class.getMethod("getApplicationProtocol");
                }
              });
      getEngineApplicationProtocol.invoke(engine);

      Method setApplicationProtocols =
          AccessController.doPrivileged(
              new PrivilegedExceptionAction<Method>() {
                @Override
                public Method run() throws Exception {
                  return SSLParameters.class.getMethod("setApplicationProtocols", String[].class);
                }
              });
      Method getApplicationProtocol =
          AccessController.doPrivileged(
              new PrivilegedExceptionAction<Method>() {
                @Override
                public Method run() throws Exception {
                  return SSLSocket.class.getMethod("getApplicationProtocol");
                }
              });
      return new JdkAlpnPlatform(sslProvider, setApplicationProtocols, getApplicationProtocol);
    } catch (NoSuchAlgorithmException ignored) {
    } catch (KeyManagementException ignored) {
    } catch (PrivilegedActionException ignored) {
    } catch (IllegalAccessException ignored) {
    } catch (InvocationTargetException ignored) {
    }

    // Find Jetty's ALPN extension for OpenJDK.
    try {
      String negoClassName = "org.eclipse.jetty.alpn.ALPN";
      Class<?> negoClass = Class.forName(negoClassName);
      Class<?> providerClass = Class.forName(negoClassName + "$Provider");
      Class<?> clientProviderClass = Class.forName(negoClassName + "$ClientProvider");
      Class<?> serverProviderClass = Class.forName(negoClassName + "$ServerProvider");
      Method putMethod = negoClass.getMethod("put", SSLSocket.class, providerClass);
      Method getMethod = negoClass.getMethod("get", SSLSocket.class);
      Method removeMethod = negoClass.getMethod("remove", SSLSocket.class);
      return new JdkWithJettyBootPlatform(
          putMethod, getMethod, removeMethod, clientProviderClass, serverProviderClass,
          sslProvider);
    } catch (ClassNotFoundException ignored) {
    } catch (NoSuchMethodException ignored) {
    }

    // TODO(ericgribkoff) Return null here
    return new Platform(sslProvider);
  }

  private static boolean isAtLeastAndroid5() {
    try {
      Platform.class
          .getClassLoader()
          .loadClass("android.net.Network"); // Arbitrary class added in Android 5.0.
      return true;
    } catch (ClassNotFoundException e) {
      logger.log(Level.FINE, "Can't find class", e);
    }
    return false;
  }

  private static boolean isAtLeastAndroid41() {
    try {
      Platform.class
          .getClassLoader()
          .loadClass("android.app.ActivityOptions"); // Arbitrary class added in Android 4.1.
      return true;
    } catch (ClassNotFoundException e) {
      logger.log(Level.FINE, "Can't find class", e);
    }
    return false;
  }

  /**
   * Forcibly load the conscrypt security provider on AppEngine if it's available. If not fail.
   */
  private static Provider getAppEngineProvider() {
    try {
      // Forcibly load conscrypt as it is unlikely to be an installed provider on AppEngine
      return (Provider) Class.forName("org.conscrypt.OpenSSLProvider")
          .getConstructor().newInstance();
    } catch (Throwable t) {
      throw new RuntimeException("Unable to load conscrypt security provider", t);
    }
  }

  /**
   * Select the first recognized security provider according to the preference order returned by
   * {@link Security#getProviders}. If a recognized provider is not found then warn but continue.
   */
  private static Provider getAndroidSecurityProvider() {
    Provider[] providers = Security.getProviders();
    for (Provider availableProvider : providers) {
      for (String providerClassName : ANDROID_SECURITY_PROVIDERS) {
        if (providerClassName.equals(availableProvider.getClass().getName())) {
          logger.log(Level.FINE, "Found registered provider {0}", providerClassName);
          return availableProvider;
        }
      }
    }
    logger.log(Level.WARNING, "Unable to find Conscrypt");
    return null;
  }

  /** Android 2.3 or better, or AppEngine with Conscrypt. */
  private static class Android extends Platform {

    private final OptionalMethod<Socket> setUseSessionTickets;
    private final OptionalMethod<Socket> setHostname;

    // Non-null on Android 4.0+.
    private final Method trafficStatsTagSocket;
    private final Method trafficStatsUntagSocket;

    // Non-null on Android 5.0+.
    private final OptionalMethod<Socket> getAlpnSelectedProtocol;
    private final OptionalMethod<Socket> setAlpnProtocols;

    private final TlsExtensionType tlsExtensionType;

    public Android(
        OptionalMethod<Socket> setUseSessionTickets,
        OptionalMethod<Socket> setHostname,
        Method trafficStatsTagSocket,
        Method trafficStatsUntagSocket,
        OptionalMethod<Socket> getAlpnSelectedProtocol,
        OptionalMethod<Socket> setAlpnProtocols,
        Provider provider,
        TlsExtensionType tlsExtensionType) {
      super(provider);
      this.setUseSessionTickets = setUseSessionTickets;
      this.setHostname = setHostname;
      this.trafficStatsTagSocket = trafficStatsTagSocket;
      this.trafficStatsUntagSocket = trafficStatsUntagSocket;
      this.getAlpnSelectedProtocol = getAlpnSelectedProtocol;
      this.setAlpnProtocols = setAlpnProtocols;
      this.tlsExtensionType = tlsExtensionType;
    }

    @Override
    public TlsExtensionType getTlsExtensionType() {
      return tlsExtensionType;
    }

    @Override public void connectSocket(Socket socket, InetSocketAddress address,
        int connectTimeout) throws IOException {
      try {
        socket.connect(address, connectTimeout);
      } catch (SecurityException se) {
        // Before android 4.3, socket.connect could throw a SecurityException
        // if opening a socket resulted in an EACCES error.
        IOException ioException = new IOException("Exception in connect");
        ioException.initCause(se);
        throw ioException;
      }
    }

    @Override public void configureTlsExtensions(
        SSLSocket sslSocket, String hostname, List<Protocol> protocols) {
      // Enable SNI and session tickets.
      if (hostname != null) {
        setUseSessionTickets.invokeOptionalWithoutCheckedException(sslSocket, true);
        setHostname.invokeOptionalWithoutCheckedException(sslSocket, hostname);
      }

      // Enable ALPN.
      if (setAlpnProtocols.isSupported(sslSocket)) {
        Object[] parameters = { concatLengthPrefixed(protocols) };
        setAlpnProtocols.invokeWithoutCheckedException(sslSocket, parameters);
      }
    }

    @Override public String getSelectedProtocol(SSLSocket socket) {
      if (!getAlpnSelectedProtocol.isSupported(socket)) return null;

      byte[] alpnResult = (byte[]) getAlpnSelectedProtocol.invokeWithoutCheckedException(socket);
      return alpnResult != null ? new String(alpnResult, Util.UTF_8) : null;
    }

    @Override public void tagSocket(Socket socket) throws SocketException {
      if (trafficStatsTagSocket == null) return;

      try {
        trafficStatsTagSocket.invoke(null, socket);
      } catch (IllegalAccessException e) {
        throw new RuntimeException(e);
      } catch (InvocationTargetException e) {
        throw new RuntimeException(e.getCause());
      }
    }

    @Override public void untagSocket(Socket socket) throws SocketException {
      if (trafficStatsUntagSocket == null) return;

      try {
        trafficStatsUntagSocket.invoke(null, socket);
      } catch (IllegalAccessException e) {
        throw new RuntimeException(e);
      } catch (InvocationTargetException e) {
        throw new RuntimeException(e.getCause());
      }
    }
  }

  /** OpenJDK 9+. */
  private static class JdkAlpnPlatform extends Platform {
    private final Method setApplicationProtocols;
    private final Method getApplicationProtocol;

    private JdkAlpnPlatform(
        Provider provider, Method setApplicationProtocols, Method getApplicationProtocol) {
      super(provider);
      this.setApplicationProtocols = setApplicationProtocols;
      this.getApplicationProtocol = getApplicationProtocol;
    }

    @Override
    public TlsExtensionType getTlsExtensionType() {
      return TlsExtensionType.ALPN_AND_NPN;
    }

    @Override
    public void configureTlsExtensions(
        SSLSocket sslSocket, String hostname, List<Protocol> protocols) {
      SSLParameters parameters = sslSocket.getSSLParameters();
      List<String> names = new ArrayList<>(protocols.size());
      for (Protocol protocol : protocols) {
        if (protocol == Protocol.HTTP_1_0) continue; // No HTTP/1.0 for ALPN.
        names.add(protocol.toString());
      }
      try {
        setApplicationProtocols.invoke(
            parameters, new Object[] {names.toArray(new String[names.size()])});
      } catch (IllegalAccessException e) {
        throw new RuntimeException(e);
      } catch (InvocationTargetException e) {
        throw new RuntimeException(e);
      }
      sslSocket.setSSLParameters(parameters);
    }

    /** Returns the negotiated protocol, or null if no protocol was negotiated. */
    @Override
    public String getSelectedProtocol(SSLSocket socket) {
      try {
        return (String) getApplicationProtocol.invoke(socket);
      } catch (IllegalAccessException e) {
        throw new RuntimeException(e);
      } catch (InvocationTargetException e) {
        throw new RuntimeException(e);
      }
    }
  }

  /**
   * OpenJDK 7+ with {@code org.mortbay.jetty.alpn/alpn-boot} in the boot class path.
   */
  private static class JdkWithJettyBootPlatform extends Platform {
    private final Method putMethod;
    private final Method getMethod;
    private final Method removeMethod;
    private final Class<?> clientProviderClass;
    private final Class<?> serverProviderClass;

    public JdkWithJettyBootPlatform(Method putMethod, Method getMethod, Method removeMethod,
        Class<?> clientProviderClass, Class<?> serverProviderClass, Provider provider) {
      super(provider);
      this.putMethod = putMethod;
      this.getMethod = getMethod;
      this.removeMethod = removeMethod;
      this.clientProviderClass = clientProviderClass;
      this.serverProviderClass = serverProviderClass;
    }

    @Override
    public TlsExtensionType getTlsExtensionType() {
      return TlsExtensionType.ALPN_AND_NPN;
    }

    @Override public void configureTlsExtensions(
        SSLSocket sslSocket, String hostname, List<Protocol> protocols) {
      List<String> names = new ArrayList<>(protocols.size());
      for (int i = 0, size = protocols.size(); i < size; i++) {
        Protocol protocol = protocols.get(i);
        if (protocol == Protocol.HTTP_1_0) continue; // No HTTP/1.0 for ALPN.
        names.add(protocol.toString());
      }
      try {
        Object provider = Proxy.newProxyInstance(Platform.class.getClassLoader(),
            new Class<?>[] { clientProviderClass, serverProviderClass }, new JettyNegoProvider(names));
        putMethod.invoke(null, sslSocket, provider);
      } catch (InvocationTargetException e) {
        throw new AssertionError(e);
      } catch (IllegalAccessException e) {
        throw new AssertionError(e);
      }
    }

    @Override public void afterHandshake(SSLSocket sslSocket) {
      try {
        removeMethod.invoke(null, sslSocket);
      } catch (IllegalAccessException ignored) {
        throw new AssertionError();
      } catch (InvocationTargetException ignored) {
      }
    }

    @Override public String getSelectedProtocol(SSLSocket socket) {
      try {
        JettyNegoProvider provider =
            (JettyNegoProvider) Proxy.getInvocationHandler(getMethod.invoke(null, socket));
        if (!provider.unsupported && provider.selected == null) {
          logger.log(Level.INFO, "ALPN callback dropped: SPDY and HTTP/2 are disabled. "
              + "Is alpn-boot on the boot class path?");
          return null;
        }
        return provider.unsupported ? null : provider.selected;
      } catch (InvocationTargetException e) {
        throw new AssertionError();
      } catch (IllegalAccessException e) {
        throw new AssertionError();
      }
    }
  }

  /**
   * Handle the methods of ALPN's ClientProvider and ServerProvider
   * without a compile-time dependency on those interfaces.
   */
  private static class JettyNegoProvider implements InvocationHandler {
    /** This peer's supported protocols. */
    private final List<String> protocols;
    /** Set when remote peer notifies ALPN is unsupported. */
    private boolean unsupported;
    /** The protocol the server selected. */
    private String selected;

    public JettyNegoProvider(List<String> protocols) {
      this.protocols = protocols;
    }

    @Override public Object invoke(Object proxy, Method method, Object[] args) throws Throwable {
      String methodName = method.getName();
      Class<?> returnType = method.getReturnType();
      if (args == null) {
        args = Util.EMPTY_STRING_ARRAY;
      }
      if (methodName.equals("supports") && boolean.class == returnType) {
        return true; // ALPN is supported.
      } else if (methodName.equals("unsupported") && void.class == returnType) {
        this.unsupported = true; // Peer doesn't support ALPN.
        return null;
      } else if (methodName.equals("protocols") && args.length == 0) {
        return protocols; // Client advertises these protocols.
      } else if ((methodName.equals("selectProtocol") || methodName.equals("select"))
          && String.class == returnType && args.length == 1 && args[0] instanceof List) {
        @SuppressWarnings("unchecked")
        List<String> peerProtocols = (List) args[0];
        // Pick the first known protocol the peer advertises.
        for (int i = 0, size = peerProtocols.size(); i < size; i++) {
          if (protocols.contains(peerProtocols.get(i))) {
            return selected = peerProtocols.get(i);
          }
        }
        return selected = protocols.get(0); // On no intersection, try peer's first protocol.
      } else if ((methodName.equals("protocolSelected") || methodName.equals("selected"))
          && args.length == 1) {
        this.selected = (String) args[0]; // Server selected this protocol.
        return null;
      } else {
        return method.invoke(this, args);
      }
    }
  }

  /**
   * Returns the concatenation of 8-bit, length prefixed protocol names.
   * http://tools.ietf.org/html/draft-agl-tls-nextprotoneg-04#page-4
   */
  public static byte[] concatLengthPrefixed(List<Protocol> protocols) {
    Buffer result = new Buffer();
    for (int i = 0, size = protocols.size(); i < size; i++) {
      Protocol protocol = protocols.get(i);
      if (protocol == Protocol.HTTP_1_0) continue; // No HTTP/1.0 for ALPN.
      result.writeByte(protocol.toString().length());
      result.writeUtf8(protocol.toString());
    }
    return result.readByteArray();
  }
}
