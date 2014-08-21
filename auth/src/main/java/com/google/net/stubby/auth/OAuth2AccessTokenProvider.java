package com.google.net.stubby.auth;

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
