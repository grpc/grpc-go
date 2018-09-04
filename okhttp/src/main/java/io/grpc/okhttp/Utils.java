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

package io.grpc.okhttp;

import com.google.common.base.Preconditions;
import io.grpc.InternalChannelz;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.internal.TransportFrameUtil;
import io.grpc.okhttp.internal.CipherSuite;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.framed.Header;
import java.net.Socket;
import java.net.SocketException;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Common utility methods for OkHttp transport.
 */
class Utils {
  private static final Logger log = Logger.getLogger(Utils.class.getName());

  static final int DEFAULT_WINDOW_SIZE = 65535;
  static final int CONNECTION_STREAM_ID = 0;

  public static Metadata convertHeaders(List<Header> http2Headers) {
    return InternalMetadata.newMetadata(convertHeadersToArray(http2Headers));
  }

  public static Metadata convertTrailers(List<Header> http2Headers) {
    return InternalMetadata.newMetadata(convertHeadersToArray(http2Headers));
  }

  private static byte[][] convertHeadersToArray(List<Header> http2Headers) {
    byte[][] headerValues = new byte[http2Headers.size() * 2][];
    int i = 0;
    for (Header header : http2Headers) {
      headerValues[i++] = header.name.toByteArray();
      headerValues[i++] = header.value.toByteArray();
    }
    return TransportFrameUtil.toRawSerializedHeaders(headerValues);
  }

  /**
   * Converts an instance of {@link com.squareup.okhttp.ConnectionSpec} for a secure connection into
   * that of {@link ConnectionSpec} in the current package.
   *
   * @throws IllegalArgumentException
   *         If {@code spec} is not with TLS
   */
  static ConnectionSpec convertSpec(com.squareup.okhttp.ConnectionSpec spec) {
    Preconditions.checkArgument(spec.isTls(), "plaintext ConnectionSpec is not accepted");

    List<com.squareup.okhttp.TlsVersion> tlsVersionList = spec.tlsVersions();
    String[] tlsVersions = new String[tlsVersionList.size()];
    for (int i = 0; i < tlsVersions.length; i++) {
      tlsVersions[i] = tlsVersionList.get(i).javaName();
    }

    List<com.squareup.okhttp.CipherSuite> cipherSuiteList = spec.cipherSuites();
    CipherSuite[] cipherSuites = new CipherSuite[cipherSuiteList.size()];
    for (int i = 0; i < cipherSuites.length; i++) {
      cipherSuites[i] = CipherSuite.valueOf(cipherSuiteList.get(i).name());
    }

    return new ConnectionSpec.Builder(spec.isTls())
        .supportsTlsExtensions(spec.supportsTlsExtensions())
        .tlsVersions(tlsVersions)
        .cipherSuites(cipherSuites)
        .build();
  }

  /**
   * Attempts to capture all known socket options and return the results as a
   * {@link InternalChannelz.SocketOptions}. If getting a socket option threw an exception,
   * log the error to the logger and report the value as an error in the response.
   */
  static InternalChannelz.SocketOptions getSocketOptions(Socket socket) {
    InternalChannelz.SocketOptions.Builder builder = new InternalChannelz.SocketOptions.Builder();
    try {
      builder.setSocketOptionLingerSeconds(socket.getSoLinger());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_LINGER", "channelz_internal_error");
    }

    try {
      builder.setSocketOptionTimeoutMillis(socket.getSoTimeout());
    } catch (Exception e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_TIMEOUT", "channelz_internal_error");
    }

    try {
      builder.addOption("TCP_NODELAY", socket.getTcpNoDelay());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("TCP_NODELAY", "channelz_internal_error");
    }

    try {
      builder.addOption("SO_REUSEADDR", socket.getReuseAddress());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_REUSEADDR", "channelz_internal_error");
    }

    try {
      builder.addOption("SO_SNDBUF", socket.getSendBufferSize());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_SNDBUF", "channelz_internal_error");
    }

    try {
      builder.addOption("SO_RECVBUF", socket.getReceiveBufferSize());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_RECVBUF", "channelz_internal_error");
    }

    try {
      builder.addOption("SO_KEEPALIVE", socket.getKeepAlive());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_KEEPALIVE", "channelz_internal_error");
    }

    try {
      builder.addOption("SO_OOBINLINE", socket.getOOBInline());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("SO_OOBINLINE", "channelz_internal_error");
    }

    try {
      builder.addOption("IP_TOS", socket.getTrafficClass());
    } catch (SocketException e) {
      log.log(Level.SEVERE, "Exception caught while reading socket option", e);
      builder.addOption("IP_TOS", "channelz_internal_error");
    }
    return builder.build();
  }

  private Utils() {
    // Prevents instantiation
  }
}
