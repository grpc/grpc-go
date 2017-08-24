/*
 * Copyright 2015, gRPC Authors All rights reserved.
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
        // Use the ALPN cause since it is prefered.
        throw new IllegalArgumentException(
            "Jetty ALPN/NPN has not been properly configured.",
            JettyTlsUtil.getJettyAlpnUnavailabilityCause());
      }
      case OPENSSL: {
        if (!OpenSsl.isAvailable()) {
          throw new IllegalArgumentException(
              "OpenSSL is not installed on the system.", OpenSsl.unavailabilityCause());
        }
        return OpenSsl.isAlpnSupported() ? NPN_AND_ALPN : NPN;
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
