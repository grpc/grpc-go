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

package io.grpc.testing;

import io.grpc.ExperimentalApi;
import io.grpc.ForwardingServerCall.SimpleForwardingServerCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.Status;

import java.io.BufferedInputStream;
import java.io.BufferedWriter;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileWriter;
import java.io.IOException;
import java.io.InputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.UnknownHostException;
import java.security.KeyStore;
import java.security.NoSuchAlgorithmException;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.concurrent.atomic.AtomicReference;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManagerFactory;
import javax.security.auth.x500.X500Principal;

/**
 * Common utility functions useful for writing tests.
 */
@ExperimentalApi
public class TestUtils {
  public static final String TEST_SERVER_HOST = "foo.test.google.fr";

  /**
   * Echo the request headers from a client into response headers and trailers. Useful for
   * testing end-to-end metadata propagation.
   */
  public static ServerInterceptor echoRequestHeadersInterceptor(final Metadata.Key<?>... keys) {
    final Set<Metadata.Key<?>> keySet = new HashSet<Metadata.Key<?>>(Arrays.asList(keys));
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          ServerCall<RespT> call,
          final Metadata requestHeaders,
          ServerCallHandler<ReqT, RespT> next) {
        return next.startCall(method,
            new SimpleForwardingServerCall<RespT>(call) {
              @Override
              public void sendHeaders(Metadata responseHeaders) {
                responseHeaders.merge(requestHeaders, keySet);
                super.sendHeaders(responseHeaders);
              }

              @Override
              public void close(Status status, Metadata trailers) {
                trailers.merge(requestHeaders, keySet);
                super.close(status, trailers);
              }
            }, requestHeaders);
      }
    };
  }

  /**
   * Capture the request headers from a client. Useful for testing metadata propagation without
   * requiring that it be symmetric on client and server, as with
   * {@link #echoRequestHeadersInterceptor}.
   */
  public static ServerInterceptor recordRequestHeadersInterceptor(
      final AtomicReference<Metadata> headersCapture) {
    return new ServerInterceptor() {
      @Override
      public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
          MethodDescriptor<ReqT, RespT> method,
          ServerCall<RespT> call,
          Metadata requestHeaders,
          ServerCallHandler<ReqT, RespT> next) {
        headersCapture.set(requestHeaders);
        return next.startCall(method, call, requestHeaders);
      }
    };
  }

  /**
   * Picks an unused port.
   */
  public static int pickUnusedPort() {
    try {
      ServerSocket serverSocket = new ServerSocket(0);
      int port = serverSocket.getLocalPort();
      serverSocket.close();
      return port;
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }

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
    List<String> ciphersMinusGcm = new ArrayList<String>();
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
   * Load a file from the resources folder.
   *
   * @param name  name of a file in src/main/resources/certs.
   */
  public static File loadCert(String name) throws IOException {
    InputStream in = TestUtils.class.getResourceAsStream("/certs/" + name);
    File tmpFile = File.createTempFile(name, "");
    tmpFile.deleteOnExit();

    BufferedWriter writer = new BufferedWriter(new FileWriter(tmpFile));
    try {
      int b;
      while ((b = in.read()) != -1) {
        writer.write(b);
      }
    } finally {
      writer.close();
    }

    return tmpFile;
  }

  /**
   * Creates an SSLSocketFactory which contains {@code certChainFile} as its only root certificate.
   */
  public static SSLSocketFactory newSslSocketFactoryForCa(File certChainFile) throws Exception {
    KeyStore ks = KeyStore.getInstance("JKS");
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
    SSLContext context = SSLContext.getInstance("TLS");
    context.init(null, trustManagerFactory.getTrustManagers(), null);
    return context.getSocketFactory();
  }

  private TestUtils() {}
}
