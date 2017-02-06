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

import static com.google.common.base.Preconditions.checkArgument;

import com.google.errorprone.annotations.CanIgnoreReturnValue;
import io.grpc.ExperimentalApi;
import io.netty.handler.codec.http2.Http2SecurityUtil;
import io.netty.handler.ssl.ApplicationProtocolConfig;
import io.netty.handler.ssl.ApplicationProtocolConfig.Protocol;
import io.netty.handler.ssl.ApplicationProtocolConfig.SelectedListenerFailureBehavior;
import io.netty.handler.ssl.ApplicationProtocolConfig.SelectorFailureBehavior;
import io.netty.handler.ssl.ApplicationProtocolNegotiator;
import io.netty.handler.ssl.OpenSsl;
import io.netty.handler.ssl.SslContextBuilder;
import io.netty.handler.ssl.SslProvider;
import io.netty.handler.ssl.SupportedCipherSuiteFilter;
import java.io.File;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * Utility for configuring SslContext for gRPC.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1784")
public class GrpcSslContexts {
  private GrpcSslContexts() {}

  /*
   * The experimental "grpc-exp" string identifies gRPC (and by implication
   * HTTP/2) when used over TLS. This indicates to the server that the client
   * will only send gRPC traffic on the h2 connection and is negotiated in
   * preference to h2 when the client and server support it, but is not
   * standardized. Support for this may be removed at any time.
   */
  private static final String GRPC_EXP_VERSION = "grpc-exp";

  // The "h2" string identifies HTTP/2 when used over TLS
  private static final String HTTP2_VERSION = "h2";

  /*
   * List of ALPN/NPN protocols in order of preference. GRPC_EXP_VERSION
   * requires that HTTP2_VERSION be present and that GRPC_EXP_VERSION should be
   * preferenced over HTTP2_VERSION.
   */
  static final List<String> NEXT_PROTOCOL_VERSIONS =
      Collections.unmodifiableList(Arrays.asList(GRPC_EXP_VERSION, HTTP2_VERSION));

  /*
   * These configs use ACCEPT due to limited support in OpenSSL.  Actual protocol enforcement is
   * done in ProtocolNegotiators.
   */
  private static final ApplicationProtocolConfig ALPN = new ApplicationProtocolConfig(
      Protocol.ALPN,
      SelectorFailureBehavior.NO_ADVERTISE,
      SelectedListenerFailureBehavior.ACCEPT,
      NEXT_PROTOCOL_VERSIONS);

  private static final ApplicationProtocolConfig NPN = new ApplicationProtocolConfig(
      Protocol.NPN,
      SelectorFailureBehavior.NO_ADVERTISE,
      SelectedListenerFailureBehavior.ACCEPT,
      NEXT_PROTOCOL_VERSIONS);

  private static final ApplicationProtocolConfig NPN_AND_ALPN = new ApplicationProtocolConfig(
      Protocol.NPN_AND_ALPN,
      SelectorFailureBehavior.NO_ADVERTISE,
      SelectedListenerFailureBehavior.ACCEPT,
      NEXT_PROTOCOL_VERSIONS);

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
  @CanIgnoreReturnValue
  public static SslContextBuilder configure(SslContextBuilder builder) {
    return configure(builder, defaultSslProvider());
  }

  /**
   * Set ciphers and APN appropriate for gRPC. Precisely what is set is permitted to change, so if
   * an application requires particular settings it should override the options set here.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1784")
  @CanIgnoreReturnValue
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

  static void ensureAlpnAndH2Enabled(ApplicationProtocolNegotiator alpnNegotiator) {
    checkArgument(alpnNegotiator != null, "ALPN must be configured");
    checkArgument(alpnNegotiator.protocols() != null && !alpnNegotiator.protocols().isEmpty(),
        "ALPN must be enabled and list HTTP/2 as a supported protocol.");
    checkArgument(
        alpnNegotiator.protocols().contains(HTTP2_VERSION),
        "This ALPN config does not support HTTP/2. Expected %s, but got %s'.",
        HTTP2_VERSION,
        alpnNegotiator.protocols());
  }
}
