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
import com.google.common.base.Preconditions;

import java.io.IOException;
import java.util.concurrent.Executor;

import javax.inject.Provider;

/**
 * A {@link Provider} that produces valid OAuth2 access tokens given a {@link Credential}.
 */
class OAuth2AccessTokenProvider implements Provider<String> {
  private static final int LOWER_BOUND_ON_EXPIRATION_SECS = 5;
  private static final int LOWER_BOUND_ON_ASYNC_REFRESH_SECS = 60;

  /**
   * Note that any call to Credential may block for long periods of time, due to shared lock with
   * refreshToken().
   */
  private final Credential credential;
  private final Executor executor;
  private String accessToken;
  private Long expiresInSeconds;
  private boolean refreshing;

  public OAuth2AccessTokenProvider(Credential credential, Executor executor) {
    this.credential = Preconditions.checkNotNull(credential);
    this.executor = Preconditions.checkNotNull(executor);
    // Access expiration before token to gracefully deals with races.
    expiresInSeconds = credential.getExpiresInSeconds();
    accessToken = credential.getAccessToken();
  }

  private synchronized boolean isAccessTokenEffectivelyExpired() {
    return accessToken == null || expiresInSeconds == null
        || expiresInSeconds <= LOWER_BOUND_ON_EXPIRATION_SECS;
  }

  private synchronized boolean isAccessTokenSoonToBeExpired() {
    return accessToken == null || expiresInSeconds == null
        || expiresInSeconds <= LOWER_BOUND_ON_ASYNC_REFRESH_SECS;
  }

  @Override
  public synchronized String get() {
    if (isAccessTokenEffectivelyExpired()) {
      // Token is at best going to expire momentarily, so we must refresh before proceeding.
      makeSureTokenIsRefreshing();
      waitForTokenRefresh();
      if (isAccessTokenEffectivelyExpired()) {
        throw new RuntimeException("Could not obtain access token");
      }
    } else if (isAccessTokenSoonToBeExpired()) {
      makeSureTokenIsRefreshing();
    }
    return accessToken;
  }

  private synchronized void makeSureTokenIsRefreshing() {
    if (refreshing) {
      return;
    }
    refreshing = true;
    executor.execute(new Runnable() {
      @Override
      public void run() {
        // Check if some other user of credential has already refreshed the access token.
        synchronized (OAuth2AccessTokenProvider.this) {
          expiresInSeconds = credential.getExpiresInSeconds();
          accessToken = credential.getAccessToken();
          if (!isAccessTokenSoonToBeExpired()) {
            refreshing = false;
            OAuth2AccessTokenProvider.this.notifyAll();
            return;
          }
        }
        try {
          credential.refreshToken();
        } catch (IOException ioe) {
          throw new RuntimeException(ioe);
        } finally {
          synchronized (OAuth2AccessTokenProvider.this) {
            refreshing = false;
            expiresInSeconds = credential.getExpiresInSeconds();
            accessToken = credential.getAccessToken();
            OAuth2AccessTokenProvider.this.notifyAll();
          }
        }
      }
    });
  }

  private synchronized void waitForTokenRefresh() {
    while (refreshing) {
      try {
        wait();
      } catch (InterruptedException ex) {
        Thread.currentThread().interrupt();
        throw new RuntimeException(ex);
      }
    }
  }
}
