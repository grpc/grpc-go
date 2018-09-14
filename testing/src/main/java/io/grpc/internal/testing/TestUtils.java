/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.internal.testing;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.UnknownHostException;
import java.security.KeyStore;
import java.security.NoSuchAlgorithmException;
import java.security.Provider;
import java.security.Security;
import java.security.cert.CertificateException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.TimeUnit;
import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManagerFactory;
import javax.security.auth.x500.X500Principal;

/**
 * Internal utility functions useful for writing tests.
 */
public class TestUtils {
  public static final String TEST_SERVER_HOST = "foo.test.google.fr";

  /**
   * Creates a new {@link InetSocketAddress} that overrides the host with {@link #TEST_SERVER_HOST}.
   */
  public static InetSocketAddress testServerAddress(String host, int port) {
    try {
      InetAddress inetAddress = InetAddress.getByName(host);
      inetAddress = InetAddress.getByAddress(TEST_SERVER_HOST, inetAddress.getAddress());
      return new InetSocketAddress(inetAddress, port);
    } catch (UnknownHostException e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * Creates a new {@link InetSocketAddress} on localhost that overrides the host with
   * {@link #TEST_SERVER_HOST}.
   */
  public static InetSocketAddress testServerAddress(int port) {
    try {
      InetAddress inetAddress = InetAddress.getByName("localhost");
      inetAddress = InetAddress.getByAddress(TEST_SERVER_HOST, inetAddress.getAddress());
      return new InetSocketAddress(inetAddress, port);
    } catch (UnknownHostException e) {
      throw new RuntimeException(e);
    }
  }

  /**
   * Returns the ciphers preferred to use during tests. They may be chosen because they are widely
   * available or because they are fast. There is no requirement that they provide confidentiality
   * or integrity.
   */
  public static List<String> preferredTestCiphers() {
    String[] ciphers;
    try {
      ciphers = SSLContext.getDefault().getDefaultSSLParameters().getCipherSuites();
    } catch (NoSuchAlgorithmException ex) {
      throw new RuntimeException(ex);
    }
    List<String> ciphersMinusGcm = new ArrayList<>();
    for (String cipher : ciphers) {
      // The GCM implementation in Java is _very_ slow (~1 MB/s)
      if (cipher.contains("_GCM_")) {
        continue;
      }
      ciphersMinusGcm.add(cipher);
    }
    return Collections.unmodifiableList(ciphersMinusGcm);
  }

  /**
   * Saves a file from the classpath resources in src/main/resources/certs as a file on the
   * filesystem.
   *
   * @param name  name of a file in src/main/resources/certs.
   */
  public static File loadCert(String name) throws IOException {
    InputStream
        in = new BufferedInputStream(TestUtils.class.getResourceAsStream("/certs/" + name));
    File tmpFile = File.createTempFile(name, "");
    tmpFile.deleteOnExit();

    OutputStream os = new BufferedOutputStream(new FileOutputStream(tmpFile));
    try {
      int b;
      while ((b = in.read()) != -1) {
        os.write(b);
      }
      os.flush();
    } finally {
      in.close();
      os.close();
    }

    return tmpFile;
  }

  /**
   * Loads an X.509 certificate from the classpath resources in src/main/resources/certs.
   *
   * @param fileName  name of a file in src/main/resources/certs.
   */
  public static X509Certificate loadX509Cert(String fileName)
      throws CertificateException, IOException {
    CertificateFactory cf = CertificateFactory.getInstance("X.509");

    InputStream in = TestUtils.class.getResourceAsStream("/certs/" + fileName);
    try {
      return (X509Certificate) cf.generateCertificate(in);
    } finally {
      in.close();
    }
  }

  private static boolean conscryptInstallAttempted;

  /**
   * Add Conscrypt to the list of security providers, if it is available. If it appears to be
   * available but fails to load, this method will throw an exception. Since the list of security
   * providers is static, this method does nothing if the provider is not available or succeeded
   * previously.
   */
  public static void installConscryptIfAvailable() {
    if (conscryptInstallAttempted) {
      return;
    }
    Class<?> conscrypt;
    try {
      conscrypt = Class.forName("org.conscrypt.Conscrypt");
    } catch (ClassNotFoundException ex) {
      conscryptInstallAttempted = true;
      return;
    }
    Method newProvider;
    try {
      newProvider = conscrypt.getMethod("newProvider");
    } catch (NoSuchMethodException ex) {
      throw new RuntimeException("Could not find newProvider method on Conscrypt", ex);
    }
    Provider provider;
    try {
      provider = (Provider) newProvider.invoke(null);
    } catch (IllegalAccessException ex) {
      throw new RuntimeException("Could not invoke Conscrypt.newProvider", ex);
    } catch (InvocationTargetException ex) {
      throw new RuntimeException("Could not invoke Conscrypt.newProvider", ex);
    }
    Security.addProvider(provider);
    conscryptInstallAttempted = true;
  }

  /**
   * Creates an SSLSocketFactory which contains {@code certChainFile} as its only root certificate.
   */
  public static SSLSocketFactory newSslSocketFactoryForCa(Provider provider,
                                                          File certChainFile) throws Exception {
    KeyStore ks = KeyStore.getInstance(KeyStore.getDefaultType());
    ks.load(null, null);
    CertificateFactory cf = CertificateFactory.getInstance("X.509");
    X509Certificate cert = (X509Certificate) cf.generateCertificate(
        new BufferedInputStream(new FileInputStream(certChainFile)));
    X500Principal principal = cert.getSubjectX500Principal();
    ks.setCertificateEntry(principal.getName("RFC2253"), cert);

    // Set up trust manager factory to use our key store.
    TrustManagerFactory trustManagerFactory =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    trustManagerFactory.init(ks);
    SSLContext context = SSLContext.getInstance("TLS", provider);
    context.init(null, trustManagerFactory.getTrustManagers(), null);
    return context.getSocketFactory();
  }

  /**
   * Sleeps for at least the specified time. When in need of a guaranteed sleep time, use this in
   * preference to {@code Thread.sleep} which might not sleep for the required time.
   */
  public static void sleepAtLeast(long millis) throws InterruptedException {
    long delay = TimeUnit.MILLISECONDS.toNanos(millis);
    long end = System.nanoTime() + delay;
    while (delay > 0) {
      TimeUnit.NANOSECONDS.sleep(delay);
      delay = end - System.nanoTime();
    }
  }

  private TestUtils() {}
}
