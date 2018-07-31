/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.auth;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.auth.Credentials;
import com.google.auth.RequestMetadataCallback;
import com.google.common.annotations.VisibleForTesting;
import com.google.common.io.BaseEncoding;
import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.SecurityLevel;
import io.grpc.Status;
import io.grpc.StatusException;
import java.io.IOException;
import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.net.URI;
import java.net.URISyntaxException;
import java.security.PrivateKey;
import java.util.Collection;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Wraps {@link Credentials} as a {@link CallCredentials}.
 */
final class GoogleAuthLibraryCallCredentials implements CallCredentials {
  private static final Logger log
      = Logger.getLogger(GoogleAuthLibraryCallCredentials.class.getName());
  private static final JwtHelper jwtHelper
      = createJwtHelperOrNull(GoogleAuthLibraryCallCredentials.class.getClassLoader());
  private static final Class<? extends Credentials> googleCredentialsClass
      = loadGoogleCredentialsClass();

  private final boolean requirePrivacy;
  @VisibleForTesting
  final Credentials creds;

  private Metadata lastHeaders;
  private Map<String, List<String>> lastMetadata;

  public GoogleAuthLibraryCallCredentials(Credentials creds) {
    this(creds, jwtHelper);
  }

  @VisibleForTesting
  GoogleAuthLibraryCallCredentials(Credentials creds, JwtHelper jwtHelper) {
    checkNotNull(creds, "creds");
    boolean requirePrivacy = false;
    if (googleCredentialsClass != null) {
      // All GoogleCredentials instances are bearer tokens and should only be used on private
      // channels. This catches all return values from GoogleCredentials.getApplicationDefault().
      // This should be checked before upgrading the Service Account to JWT, as JWT is also a bearer
      // token.
      requirePrivacy = googleCredentialsClass.isInstance(creds);
    }
    if (jwtHelper != null) {
      creds = jwtHelper.tryServiceAccountToJwt(creds);
    }
    this.requirePrivacy = requirePrivacy;
    this.creds = creds;
  }

  @Override
  public void thisUsesUnstableApi() {}

  @Override
  public void applyRequestMetadata(MethodDescriptor<?, ?> method, Attributes attrs,
      Executor appExecutor, final MetadataApplier applier) {
    SecurityLevel security = attrs.get(ATTR_SECURITY_LEVEL);
    if (security == null) {
      // Although the API says ATTR_SECURITY_LEVEL is required, no one was really looking at it thus
      // there may be transports that got away without setting it.  Now we start to check it, it'd
      // be less disruptive to tolerate nulls.
      security = SecurityLevel.NONE;
    }
    if (requirePrivacy && security != SecurityLevel.PRIVACY_AND_INTEGRITY) {
      applier.fail(Status.UNAUTHENTICATED
          .withDescription("Credentials require channel with PRIVACY_AND_INTEGRITY security level. "
              + "Observed security level: " + security));
      return;
    }

    String authority = checkNotNull(attrs.get(ATTR_AUTHORITY), "authority");
    final URI uri;
    try {
      uri = serviceUri(authority, method);
    } catch (StatusException e) {
      applier.fail(e.getStatus());
      return;
    }
    // Credentials is expected to manage caching internally if the metadata is fetched over
    // the network.
    creds.getRequestMetadata(uri, appExecutor, new RequestMetadataCallback() {
      @Override
      public void onSuccess(Map<String, List<String>> metadata) {
        // Some implementations may pass null metadata.

        // Re-use the headers if getRequestMetadata() returns the same map. It may return a
        // different map based on the provided URI, i.e., for JWT. However, today it does not
        // cache JWT and so we won't bother tring to save its return value based on the URI.
        Metadata headers;
        try {
          synchronized (GoogleAuthLibraryCallCredentials.this) {
            if (lastMetadata == null || lastMetadata != metadata) {
              lastHeaders = toHeaders(metadata);
              lastMetadata = metadata;
            }
            headers = lastHeaders;
          }
        } catch (Throwable t) {
          applier.fail(Status.UNAUTHENTICATED
              .withDescription("Failed to convert credential metadata")
              .withCause(t));
          return;
        }
        applier.apply(headers);
      }

      @Override
      public void onFailure(Throwable e) {
        if (e instanceof IOException) {
          // Since it's an I/O failure, let the call be retried with UNAVAILABLE.
          applier.fail(Status.UNAVAILABLE
              .withDescription("Credentials failed to obtain metadata")
              .withCause(e));
        } else {
          applier.fail(Status.UNAUTHENTICATED
              .withDescription("Failed computing credential metadata")
              .withCause(e));
        }
      }
    });
  }

  /**
   * Generate a JWT-specific service URI. The URI is simply an identifier with enough information
   * for a service to know that the JWT was intended for it. The URI will commonly be verified with
   * a simple string equality check.
   */
  private static URI serviceUri(String authority, MethodDescriptor<?, ?> method)
      throws StatusException {
    // Always use HTTPS, by definition.
    final String scheme = "https";
    final int defaultPort = 443;
    String path = "/" + MethodDescriptor.extractFullServiceName(method.getFullMethodName());
    URI uri;
    try {
      uri = new URI(scheme, authority, path, null, null);
    } catch (URISyntaxException e) {
      throw Status.UNAUTHENTICATED.withDescription("Unable to construct service URI for auth")
          .withCause(e).asException();
    }
    // The default port must not be present. Alternative ports should be present.
    if (uri.getPort() == defaultPort) {
      uri = removePort(uri);
    }
    return uri;
  }

  private static URI removePort(URI uri) throws StatusException {
    try {
      return new URI(uri.getScheme(), uri.getUserInfo(), uri.getHost(), -1 /* port */,
          uri.getPath(), uri.getQuery(), uri.getFragment());
    } catch (URISyntaxException e) {
      throw Status.UNAUTHENTICATED.withDescription(
           "Unable to construct service URI after removing port").withCause(e).asException();
    }
  }

  @SuppressWarnings("BetaApi") // BaseEncoding is stable in Guava 20.0
  private static Metadata toHeaders(@Nullable Map<String, List<String>> metadata) {
    Metadata headers = new Metadata();
    if (metadata != null) {
      for (String key : metadata.keySet()) {
        if (key.endsWith("-bin")) {
          Metadata.Key<byte[]> headerKey = Metadata.Key.of(key, Metadata.BINARY_BYTE_MARSHALLER);
          for (String value : metadata.get(key)) {
            headers.put(headerKey, BaseEncoding.base64().decode(value));
          }
        } else {
          Metadata.Key<String> headerKey = Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER);
          for (String value : metadata.get(key)) {
            headers.put(headerKey, value);
          }
        }
      }
    }
    return headers;
  }

  @VisibleForTesting
  @Nullable
  static JwtHelper createJwtHelperOrNull(ClassLoader loader) {
    Class<?> rawServiceAccountClass;
    try {
      // Specify loader so it can be overridden in tests
      rawServiceAccountClass
          = Class.forName("com.google.auth.oauth2.ServiceAccountCredentials", false, loader);
    } catch (ClassNotFoundException ex) {
      return null;
    }
    Exception caughtException;
    try {
      return new JwtHelper(rawServiceAccountClass, loader);
    } catch (ClassNotFoundException ex) {
      caughtException = ex;
    } catch (NoSuchMethodException ex) {
      caughtException = ex;
    }
    if (caughtException != null) {
      // Failure is a bug in this class, but we still choose to gracefully recover
      log.log(Level.WARNING, "Failed to create JWT helper. This is unexpected", caughtException);
    }
    return null;
  }

  @Nullable
  private static Class<? extends Credentials> loadGoogleCredentialsClass() {
    Class<?> rawGoogleCredentialsClass;
    try {
      // Can't use a loader as it disables ProGuard's reference detection and would fail to rename
      // this reference. Unfortunately this will initialize the class.
      rawGoogleCredentialsClass = Class.forName("com.google.auth.oauth2.GoogleCredentials");
    } catch (ClassNotFoundException ex) {
      log.log(Level.FINE, "Failed to load GoogleCredentials", ex);
      return null;
    }
    return rawGoogleCredentialsClass.asSubclass(Credentials.class);
  }

  @VisibleForTesting
  static class JwtHelper {
    private final Class<? extends Credentials> serviceAccountClass;
    private final Constructor<? extends Credentials> jwtConstructor;
    private final Method getScopes;
    private final Method getClientId;
    private final Method getClientEmail;
    private final Method getPrivateKey;
    private final Method getPrivateKeyId;

    public JwtHelper(Class<?> rawServiceAccountClass, ClassLoader loader)
        throws ClassNotFoundException, NoSuchMethodException {
      serviceAccountClass = rawServiceAccountClass.asSubclass(Credentials.class);
      getScopes = serviceAccountClass.getMethod("getScopes");
      getClientId = serviceAccountClass.getMethod("getClientId");
      getClientEmail = serviceAccountClass.getMethod("getClientEmail");
      getPrivateKey = serviceAccountClass.getMethod("getPrivateKey");
      getPrivateKeyId = serviceAccountClass.getMethod("getPrivateKeyId");
      Class<? extends Credentials> jwtClass = Class.forName(
          "com.google.auth.oauth2.ServiceAccountJwtAccessCredentials", false, loader)
          .asSubclass(Credentials.class);
      jwtConstructor
          = jwtClass.getConstructor(String.class, String.class, PrivateKey.class, String.class);
    }

    public Credentials tryServiceAccountToJwt(Credentials creds) {
      if (!serviceAccountClass.isInstance(creds)) {
        return creds;
      }
      Exception caughtException;
      try {
        creds = serviceAccountClass.cast(creds);
        Collection<?> scopes = (Collection<?>) getScopes.invoke(creds);
        if (scopes.size() != 0) {
          // Leave as-is, since the scopes may limit access within the service.
          return creds;
        }
        return jwtConstructor.newInstance(
            getClientId.invoke(creds),
            getClientEmail.invoke(creds),
            getPrivateKey.invoke(creds),
            getPrivateKeyId.invoke(creds));
      } catch (IllegalAccessException ex) {
        caughtException = ex;
      } catch (InvocationTargetException ex) {
        caughtException = ex;
      } catch (InstantiationException ex) {
        caughtException = ex;
      }
      if (caughtException != null) {
        // Failure is a bug in this class, but we still choose to gracefully recover
        log.log(
            Level.WARNING,
            "Failed converting service account credential to JWT. This is unexpected",
            caughtException);
      }
      return creds;
    }
  }
}
