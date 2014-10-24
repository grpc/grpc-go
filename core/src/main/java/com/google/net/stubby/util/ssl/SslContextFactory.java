package com.google.net.stubby.util.ssl;

import java.security.KeyStore;

import javax.net.ssl.KeyManagerFactory;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManager;

/**
 * A helper class that hides the SSLContext config details for server and client.
 *
 * <p>
 * Currently it's mostly used to generate a client side SSL engine that blindly trusts any
 * certificates.
 * </p>
 *
 */
public final class SslContextFactory {

  private static final String PROTOCOL = "TLSv1";
  private static final SSLContext CLIENT_CONTEXT;
  private static final SSLContext SERVER_CONTEXT;

  static {
    SSLContext serverContext;
    SSLContext clientContext;

    try {
      // Create a SSLContext that uses a pre-genrated key store.
      KeyStore keyStore = KeyStore.getInstance("JKS");
      keyStore.load(SslKeyStore.asInputStream(), SslKeyStore.getKeyStorePassword());

      // Set up key manager factory to use our key store
      String algorithm = System.getProperty("ssl.KeyManagerFactory.algorithm");
      if (algorithm == null) {
        algorithm = "SunX509";
      }
      KeyManagerFactory keyManagerFactory = KeyManagerFactory.getInstance(algorithm);
      keyManagerFactory.init(keyStore, SslKeyStore.getCertificatePassword());

      // Initialize the SSLContext to work with our key managers.
      serverContext = SSLContext.getInstance(PROTOCOL);
      serverContext.init(keyManagerFactory.getKeyManagers(), null, null);
    } catch (Exception e) {
      throw new Error(
          "Failed to initialize the server-side SSLContext", e);
    }

    try {
      // Create a client side SSLContext that trusts any certificate.
      clientContext = SSLContext.getInstance(PROTOCOL);
      clientContext.init(null, new TrustManager[]{new InsecureTrustManager()}, null);
    } catch (Exception e) {
      throw new Error(
          "Failed to initialize the client-side SSLContext", e);
    }

    SERVER_CONTEXT = serverContext;
    CLIENT_CONTEXT = clientContext;
  }

  public static SSLContext getServerContext() {
    return SERVER_CONTEXT;
  }

  public static SSLContext getClientContext() {
    return CLIENT_CONTEXT;
  }

  private SslContextFactory() {
    // Unused
  }
}
