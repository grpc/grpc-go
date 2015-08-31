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

package io.grpc.netty;

import io.grpc.ExperimentalApi;
import io.netty.handler.codec.http2.Http2SecurityUtil;
import io.netty.handler.ssl.ApplicationProtocolConfig;
import io.netty.handler.ssl.ApplicationProtocolConfig.Protocol;
import io.netty.handler.ssl.ApplicationProtocolConfig.SelectedListenerFailureBehavior;
import io.netty.handler.ssl.ApplicationProtocolConfig.SelectorFailureBehavior;
import io.netty.handler.ssl.OpenSsl;
import io.netty.handler.ssl.SslContextBuilder;
import io.netty.handler.ssl.SslProvider;
import io.netty.handler.ssl.SupportedCipherSuiteFilter;

import java.io.File;

/**
 * Utility for configuring SslContext for gRPC.
 */
public class GrpcSslContexts {
  private GrpcSslContexts() {}

  private static String[] HTTP2_VERSIONS = {"h2"};

  private static ApplicationProtocolConfig ALPN = new ApplicationProtocolConfig(
      Protocol.ALPN,
      SelectorFailureBehavior.NO_ADVERTISE,
      SelectedListenerFailureBehavior.ACCEPT,
      HTTP2_VERSIONS);

  private static ApplicationProtocolConfig NPN = new ApplicationProtocolConfig(
      Protocol.NPN,
      SelectorFailureBehavior.NO_ADVERTISE,
      SelectedListenerFailureBehavior.ACCEPT,
      HTTP2_VERSIONS);

  private static ApplicationProtocolConfig NPN_AND_ALPN = new ApplicationProtocolConfig(
      Protocol.NPN_AND_ALPN,
      SelectorFailureBehavior.NO_ADVERTISE,
      SelectedListenerFailureBehavior.ACCEPT,
      HTTP2_VERSIONS);

  /**
   * Creates a SslContextBuilder with ciphers and APN appropriate for gRPC.
   *
   * @see SslContextBuilder#forClient()
   * @see #configure(SslContextBuilder)
   */
  public static SslContextBuilder forClient() {
    return configure(SslContextBuilder.forClient());
  }

  /**
   * Creates a SslContextBuilder with ciphers and APN appropriate for gRPC.
   *
   * @see SslContextBuilder#forServer(File, File)
   * @see #configure(SslContextBuilder)
   */
  public static SslContextBuilder forServer(File keyCertChainFile, File keyFile) {
    return configure(SslContextBuilder.forServer(keyCertChainFile, keyFile));
  }

  /**
   * Creates a SslContextBuilder with ciphers and APN appropriate for gRPC.
   *
   * @see SslContextBuilder#forServer(File, File, String)
   * @see #configure(SslContextBuilder)
   */
  public static SslContextBuilder forServer(
      File keyCertChainFile, File keyFile, String keyPassword) {
    return configure(SslContextBuilder.forServer(keyCertChainFile, keyFile, keyPassword));
  }

  /**
   * Set ciphers and APN appropriate for gRPC. Precisely what is set is permitted to change, so if
   * an application requires particular settings it should override the options set here.
   */
  public static SslContextBuilder configure(SslContextBuilder builder) {
    return configure(builder, defaultSslProvider());
  }

  /**
   * Set ciphers and APN appropriate for gRPC. Precisely what is set is permitted to change, so if
   * an application requires particular settings it should override the options set here.
   */
  @ExperimentalApi
  public static SslContextBuilder configure(SslContextBuilder builder, SslProvider provider) {
    return builder.sslProvider(provider)
                  .ciphers(Http2SecurityUtil.CIPHERS, SupportedCipherSuiteFilter.INSTANCE)
                  .applicationProtocolConfig(selectApplicationProtocolConfig(provider));
  }

  /**
   * Returns OpenSSL if available, otherwise returns the JDK provider.
   */
  private static SslProvider defaultSslProvider() {
    return OpenSsl.isAvailable() ? SslProvider.OPENSSL : SslProvider.JDK;
  }

  /**
   * Attempts to select the best {@link ApplicationProtocolConfig} for the given
   * {@link SslProvider}.
   */
  private static ApplicationProtocolConfig selectApplicationProtocolConfig(SslProvider provider) {
    switch (provider) {
      case JDK: {
        if (JettyTlsUtil.isJettyAlpnConfigured()) {
          return ALPN;
        }
        if (JettyTlsUtil.isJettyNpnConfigured()) {
          return NPN;
        }
        throw new IllegalArgumentException("Jetty ALPN/NPN has not been properly configured.");
      }
      case OPENSSL: {
        if (!OpenSsl.isAvailable()) {
          throw new IllegalArgumentException("OpenSSL is not installed on the system.");
        }

        if (OpenSsl.isAlpnSupported()) {
          return NPN_AND_ALPN;
        } else {
          return NPN;
        }
      }
      default:
        throw new IllegalArgumentException("Unsupported provider: " + provider);
    }
  }
}
