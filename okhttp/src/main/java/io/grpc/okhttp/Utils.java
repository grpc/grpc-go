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

package io.grpc.okhttp;

import com.google.common.base.Preconditions;
import io.grpc.InternalMetadata;
import io.grpc.Metadata;
import io.grpc.internal.TransportFrameUtil;
import io.grpc.okhttp.internal.CipherSuite;
import io.grpc.okhttp.internal.ConnectionSpec;
import io.grpc.okhttp.internal.framed.Header;
import java.util.List;

/**
 * Common utility methods for OkHttp transport.
 */
class Utils {
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

  private Utils() {
    // Prevents instantiation
  }
}
