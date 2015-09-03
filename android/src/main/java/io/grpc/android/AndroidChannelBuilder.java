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

package io.grpc.android;

import io.grpc.ClientInterceptor;
import io.grpc.ExperimentalApi;
import io.grpc.ManagedChannelBuilder;
import io.grpc.internal.ManagedChannelImpl;
import io.grpc.okhttp.NegotiationType;
import io.grpc.okhttp.OkHttpChannelBuilder;

import java.util.List;
import java.util.concurrent.Executor;

/**
 * A {@link io.grpc.ManagedChannelBuilder} that provides a stable interface for producing
 * {@link io.grpc.Channel} instances on Android. This allows applications to be resilient
 * to changes such as defaulting to a new channel implementation or breaking API changes
 * in existing ones. Configuration options will be added here only if they provide value
 * to a broad set of users and are likely to be resilient to changes in the underlying channel
 * implementation.
 *
 * <p>Developers needing advanced configuration are free to use the underlying channel
 * implementations directly while assuming the risks associated with using an
 * {@link io.grpc.ExperimentalApi}.
 */

// TODO(lryan):
// - Document the various dangers of scheduling work onto Android Main/UI/whatever
// threads. Is any enforcement practical? Conversely is there a sensible default. I assume
// its also a bad idea to use huge numbers of threads too.
// - Provide SSL installation and detect ALPN/NPN support?
// - Allow for an alternate TrustManager ? for self-signed, pinning etc. Is there a good default
//   impl for these variations blessed by security team?
// - Augment with user-agent with app id?
// - Specify a smaller flow-control window by default? Fewer concurrent streams?
public class AndroidChannelBuilder extends ManagedChannelBuilder<AndroidChannelBuilder> {
  // Currently we only support OkHttp on Android. This will become dynamic as more transport
  // implementations become available.
  private final OkHttpChannelBuilder baseBuilder;

  public static AndroidChannelBuilder forAddress(String host, int port) {
    return new AndroidChannelBuilder(OkHttpChannelBuilder.forAddress(host, port));
  }

  private AndroidChannelBuilder(OkHttpChannelBuilder builder) {
    this.baseBuilder = builder;
  }

  @Override
  public AndroidChannelBuilder executor(Executor executor) {
    baseBuilder.executor(executor);
    return this;
  }

  @Override
  public AndroidChannelBuilder intercept(List<ClientInterceptor> list) {
    baseBuilder.intercept(list);
    return this;
  }

  @Override
  public AndroidChannelBuilder intercept(ClientInterceptor... interceptors) {
    baseBuilder.intercept(interceptors);
    return this;
  }

  @Override
  public AndroidChannelBuilder userAgent(String userAgent) {
    baseBuilder.userAgent(userAgent);
    return this;
  }

  /**
   * Use of a plaintext connection to the server. By default a secure connection mechanism
   * such as TLS will be used.
   *
   * <p>Should only be used for testing or for APIs where the use of such API or the data
   * exchanged is not sensitive.
   *
   * @param skipNegotiation @{code true} if there is a priori knowledge that the endpoint supports
   *                        plaintext, {@code false} if plaintext use must be negotiated.
   */
  @ExperimentalApi("primarily for testing")
  public AndroidChannelBuilder usePlaintext(boolean skipNegotiation) {
    if (skipNegotiation) {
      baseBuilder.negotiationType(NegotiationType.PLAINTEXT);
      return this;
    } else {
      throw new IllegalArgumentException("Not currently supported");
    }
  }

  /**
   * Overrides the host used with TLS and HTTP virtual hosting. It does not change what host is
   * actually connected to.
   *
   * <p>Should only be used for testing.
   */
  @ExperimentalApi("primarily for testing")
  public AndroidChannelBuilder overrideHostForAuthority(String host) {
    baseBuilder.overrideHostForAuthority(host);
    return this;
  }

  @Override
  public ManagedChannelImpl build() {
    return baseBuilder.build();
  }
}
