package com.google.net.stubby.auth;

import com.google.api.client.auth.oauth2.Credential;
import com.google.common.base.Preconditions;
import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.MethodDescriptor;

import java.util.concurrent.Executor;

import javax.inject.Provider;

/** Channel wrapper that authenticates all calls with OAuth2. */
public class OAuth2ChannelInterceptor implements Channel {
  private final Channel delegate;
  private final OAuth2AccessTokenProvider accessTokenProvider;
  private final Provider<String> authorizationHeaderProvider
      = new Provider<String>() {
        @Override
        public String get() {
          return "Bearer " + accessTokenProvider.get();
        }
      };

  public OAuth2ChannelInterceptor(Channel delegate, Credential credential, Executor executor) {
    this.delegate = Preconditions.checkNotNull(delegate);
    this.accessTokenProvider = new OAuth2AccessTokenProvider(credential, executor);
  }

  @Override
  public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
    // TODO(user): If the call fails for Auth reasons, this does not properly propagate info that
    // would be in WWW-Authenticate, because it does not yet have access to the header.
    return delegate.newCall(method.withHeader("Authorization", authorizationHeaderProvider));
  }
}
