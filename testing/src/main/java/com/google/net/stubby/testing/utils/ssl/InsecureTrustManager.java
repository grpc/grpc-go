package com.google.net.stubby.testing.utils.ssl;

import java.security.cert.CertificateException;
import java.security.cert.X509Certificate;

import javax.net.ssl.X509TrustManager;

/**
 * This Trust Manager does absolutely no certificate verification.
 * It's meant to be used only by test code, and should never ever be
 * used in production code.
 *
 */
public class InsecureTrustManager implements X509TrustManager {
  /**
   * @param chain The certificate chain we completely ignore and treat
   * as valid.
   * @param authType The type of certificate
   * @throws CertificateException never, even if the certificate chain
   * is invalid.
   */
  public void checkClientTrusted(X509Certificate[] chain, String authType)
      throws CertificateException {
    // Doing absolutely no checking of client certificate chain.
  }

  /**
   * @param chain The certificate chain we completely ignore and treat
   * as valid.
   * @param authType The type of certificate
   * @throws CertificateException never, even if the certificate chain
   * is invalid.
   */
  public void checkServerTrusted(X509Certificate[] chain, String authType)
      throws CertificateException {
    // Doing absolutely no checking of server certificate chain.
  }

  /**
   * @return null, always.
   */
  public X509Certificate[] getAcceptedIssuers() {
    return null;
  }
}
