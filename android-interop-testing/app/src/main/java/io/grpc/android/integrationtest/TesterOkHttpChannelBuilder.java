/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.android.integrationtest;

import android.annotation.TargetApi;
import android.net.SSLCertificateSocketFactory;
import android.os.Build;
import android.support.annotation.Nullable;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.okhttp.NegotiationType;
import io.grpc.okhttp.OkHttpChannelBuilder;

import java.io.InputStream;
import java.lang.reflect.Method;
import java.security.KeyStore;
import java.security.cert.CertificateFactory;
import java.security.cert.X509Certificate;

import javax.net.ssl.SSLContext;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.TrustManager;
import javax.net.ssl.TrustManagerFactory;
import javax.security.auth.x500.X500Principal;

/**
 * A helper class to create a OkHttp based channel.
 */
class TesterOkHttpChannelBuilder {
  public static ManagedChannel build(String host, int port, @Nullable String serverHostOverride,
      boolean useTls, @Nullable InputStream testCa, @Nullable String androidSocketFactoryTls) {
    ManagedChannelBuilder<?> channelBuilder = ManagedChannelBuilder.forAddress(host, port)
        .maxInboundMessageSize(16 * 1024 * 1024);
    if (serverHostOverride != null) {
      // Force the hostname to match the cert the server uses.
      channelBuilder.overrideAuthority(serverHostOverride);
    }
    if (useTls) {
      try {
        SSLSocketFactory factory;
        if (androidSocketFactoryTls != null) {
          factory = getSslCertificateSocketFactory(testCa, androidSocketFactoryTls);
        } else {
          factory = getSslSocketFactory(testCa);
        }
        ((OkHttpChannelBuilder) channelBuilder).negotiationType(NegotiationType.TLS);
        ((OkHttpChannelBuilder) channelBuilder).sslSocketFactory(factory);
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    } else {
      channelBuilder.usePlaintext(true);
    }
    return channelBuilder.build();
  }

  private static SSLSocketFactory getSslSocketFactory(@Nullable InputStream testCa)
      throws Exception {
    if (testCa == null) {
      return (SSLSocketFactory) SSLSocketFactory.getDefault();
    }

    SSLContext context = SSLContext.getInstance("TLS");
    context.init(null, getTrustManagers(testCa) , null);
    return context.getSocketFactory();
  }

  @TargetApi(14)
  private static SSLCertificateSocketFactory getSslCertificateSocketFactory(
      @Nullable InputStream testCa, String androidSocketFatoryTls) throws Exception {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.ICE_CREAM_SANDWICH /* API level 14 */) {
      throw new RuntimeException(
          "android_socket_factory_tls doesn't work with API level less than 14.");
    }
    SSLCertificateSocketFactory factory = (SSLCertificateSocketFactory)
        SSLCertificateSocketFactory.getDefault(5000 /* Timeout in ms*/);
    // Use HTTP/2.0
    byte[] h2 = "h2".getBytes();
    byte[][] protocols = new byte[][]{h2};
    if (androidSocketFatoryTls.equals("alpn")) {
      Method setAlpnProtocols =
          factory.getClass().getDeclaredMethod("setAlpnProtocols", byte[][].class);
      setAlpnProtocols.invoke(factory, new Object[] { protocols });
    } else if (androidSocketFatoryTls.equals("npn")) {
      Method setNpnProtocols =
          factory.getClass().getDeclaredMethod("setNpnProtocols", byte[][].class);
      setNpnProtocols.invoke(factory, new Object[]{protocols});
    } else {
      throw new RuntimeException("Unknown protocol: " + androidSocketFatoryTls);
    }

    if (testCa != null) {
      factory.setTrustManagers(getTrustManagers(testCa));
    }

    return factory;
  }

  private static TrustManager[] getTrustManagers(InputStream testCa) throws Exception {
    KeyStore ks = KeyStore.getInstance(KeyStore.getDefaultType());
    ks.load(null);
    CertificateFactory cf = CertificateFactory.getInstance("X.509");
    X509Certificate cert = (X509Certificate) cf.generateCertificate(testCa);
    X500Principal principal = cert.getSubjectX500Principal();
    ks.setCertificateEntry(principal.getName("RFC2253"), cert);
    // Set up trust manager factory to use our key store.
    TrustManagerFactory trustManagerFactory =
        TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
    trustManagerFactory.init(ks);
    return trustManagerFactory.getTrustManagers();
  }
}

