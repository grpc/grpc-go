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

package io.grpc.auth;

import com.google.api.client.auth.oauth2.Credential;

import io.grpc.Call;
import io.grpc.Channel;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors.ForwardingCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;

import java.util.concurrent.Executor;

import javax.inject.Provider;

/** Client interceptor that authenticates all calls with OAuth2. */
public class OAuth2ChannelInterceptor implements ClientInterceptor {
  private static final Metadata.Key<String> AUTHORIZATION =
      Metadata.Key.of("Authorization", Metadata.ASCII_STRING_MARSHALLER);

  private final OAuth2AccessTokenProvider accessTokenProvider;
  private final Provider<String> authorizationHeaderProvider
      = new Provider<String>() {
        @Override
        public String get() {
          return "Bearer " + accessTokenProvider.get();
        }
      };

  public OAuth2ChannelInterceptor(Credential credential, Executor executor) {
    this.accessTokenProvider = new OAuth2AccessTokenProvider(credential, executor);
  }

  @Override
  public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
      Channel next) {
    // TODO(ejona): If the call fails for Auth reasons, this does not properly propagate info that
    // would be in WWW-Authenticate, because it does not yet have access to the header.
    return new ForwardingCall<ReqT, RespT>(next.newCall(method)) {
      @Override
      public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
        headers.put(AUTHORIZATION, authorizationHeaderProvider.get());
        super.start(responseListener, headers);
      }
    };
  }
}
