/*
 * Copyright 2016, Google Inc. All rights reserved.
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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.auth.Credentials;
import com.google.common.annotations.VisibleForTesting;
import com.google.common.io.BaseEncoding;
import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.StatusException;
import java.lang.reflect.Constructor;
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
    if (jwtHelper != null) {
      creds = jwtHelper.tryServiceAccountToJwt(creds);
    }
    this.creds = creds;
  }

  @Override
  public void applyRequestMetadata(MethodDescriptor<?, ?> method, Attributes attrs,
      Executor appExecutor, final MetadataApplier applier) {
    String authority = checkNotNull(attrs.get(ATTR_AUTHORITY), "authority");
    final URI uri;
    try {
      uri = serviceUri(authority, method);
    } catch (StatusException e) {
      applier.fail(e.getStatus());
      return;
    }
    appExecutor.execute(new Runnable() {
        @Override
        public void run() {
          try {
            // Credentials is expected to manage caching internally if the metadata is fetched over
            // the network.
            //
            // TODO(zhangkun83): we don't know whether there is valid cache data. If there is, we
            // would waste a context switch by always scheduling in executor. However, we have to
            // do so because we can't risk blocking the network thread. This can be resolved after
            // https://github.com/google/google-auth-library-java/issues/3 is resolved.
            //
            // Some implementations may return null here.
            Map<String, List<String>> metadata = creds.getRequestMetadata(uri);
            // Re-use the headers if getRequestMetadata() returns the same map. It may return a
            // different map based on the provided URI, i.e., for JWT. However, today it does not
            // cache JWT and so we won't bother tring to save its return value based on the URI.
            Metadata headers;
            synchronized (GoogleAuthLibraryCallCredentials.this) {
              if (lastMetadata == null || lastMetadata != metadata) {
                lastMetadata = metadata;
                lastHeaders = toHeaders(metadata);
              }
              headers = lastHeaders;
            }
            applier.apply(headers);
          } catch (Throwable e) {
            applier.fail(Status.UNAUTHENTICATED.withCause(e));
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
    if (authority == null) {
      throw Status.UNAUTHENTICATED.withDescription("Channel has no authority").asException();
    }
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
      // Specify loader so it can be overriden in tests
      rawServiceAccountClass
          = Class.forName("com.google.auth.oauth2.ServiceAccountCredentials", false, loader);
    } catch (ClassNotFoundException ex) {
      return null;
    }
    try {
      return new JwtHelper(rawServiceAccountClass, loader);
    } catch (ReflectiveOperationException ex) {
      // Failure is a bug in this class, but we still choose to gracefully recover
      log.log(Level.WARNING, "Failed to create JWT helper. This is unexpected", ex);
      return null;
    }
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
        throws ReflectiveOperationException {
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
      } catch (ReflectiveOperationException ex) {
        // Failure is a bug in this class, but we still choose to gracefully recover
        log.log(Level.WARNING,
            "Failed converting service account credential to JWT. This is unexpected", ex);
        return creds;
      }
    }
  }
}
