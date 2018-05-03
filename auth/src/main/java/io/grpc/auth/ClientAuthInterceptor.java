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

package io.grpc.auth;

import com.google.auth.Credentials;
import com.google.common.base.Preconditions;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors.CheckedForwardingClientCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.StatusException;
import java.io.IOException;
import java.net.URI;
import java.net.URISyntaxException;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;

/**
 * Client interceptor that authenticates all calls by binding header data provided by a credential.
 * Typically this will populate the Authorization header but other headers may also be filled out.
 *
 * <p>Uses the new and simplified Google auth library:
 * https://github.com/google/google-auth-library-java
 *
 * @deprecated use {@link MoreCallCredentials#from(Credentials)} instead.
 */
@Deprecated
public final class ClientAuthInterceptor implements ClientInterceptor {

  private final Credentials credentials;

  private Metadata cached;
  private Map<String, List<String>> lastMetadata;

  public ClientAuthInterceptor(
      Credentials credentials, @SuppressWarnings("unused") Executor executor) {
    this.credentials = Preconditions.checkNotNull(credentials, "credentials");
    // TODO(louiscryan): refresh token asynchronously with this executor.
  }

  @Override
  public <ReqT, RespT> ClientCall<ReqT, RespT> interceptCall(
      final MethodDescriptor<ReqT, RespT> method, CallOptions callOptions, final Channel next) {
    // TODO(ejona86): If the call fails for Auth reasons, this does not properly propagate info that
    // would be in WWW-Authenticate, because it does not yet have access to the header.
    return new CheckedForwardingClientCall<ReqT, RespT>(next.newCall(method, callOptions)) {
      @Override
      protected void checkedStart(Listener<RespT> responseListener, Metadata headers)
          throws StatusException {
        Metadata cachedSaved;
        URI uri = serviceUri(next, method);
        synchronized (ClientAuthInterceptor.this) {
          // TODO(louiscryan): This is icky but the current auth library stores the same
          // metadata map until the next refresh cycle. This will be fixed once
          // https://github.com/google/google-auth-library-java/issues/3
          // is resolved.
          // getRequestMetadata() may return a different map based on the provided URI, i.e., for
          // JWT. However, today it does not cache JWT and so we won't bother tring to cache its
          // return value based on the URI.
          Map<String, List<String>> latestMetadata = getRequestMetadata(uri);
          if (lastMetadata == null || lastMetadata != latestMetadata) {
            lastMetadata = latestMetadata;
            cached = toHeaders(lastMetadata);
          }
          cachedSaved = cached;
        }
        headers.merge(cachedSaved);
        delegate().start(responseListener, headers);
      }
    };
  }

  /**
   * Generate a JWT-specific service URI. The URI is simply an identifier with enough information
   * for a service to know that the JWT was intended for it. The URI will commonly be verified with
   * a simple string equality check.
   */
  private URI serviceUri(Channel channel, MethodDescriptor<?, ?> method) throws StatusException {
    String authority = channel.authority();
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

  private URI removePort(URI uri) throws StatusException {
    try {
      return new URI(uri.getScheme(), uri.getUserInfo(), uri.getHost(), -1 /* port */,
          uri.getPath(), uri.getQuery(), uri.getFragment());
    } catch (URISyntaxException e) {
      throw Status.UNAUTHENTICATED.withDescription(
            "Unable to construct service URI after removing port")
          .withCause(e).asException();
    }
  }

  private Map<String, List<String>> getRequestMetadata(URI uri) throws StatusException {
    try {
      return credentials.getRequestMetadata(uri);
    } catch (IOException e) {
      throw Status.UNAUTHENTICATED.withDescription("Unable to get request metadata").withCause(e)
          .asException();
    }
  }

  private static final Metadata toHeaders(Map<String, List<String>> metadata) {
    Metadata headers = new Metadata();
    if (metadata != null) {
      for (String key : metadata.keySet()) {
        Metadata.Key<String> headerKey = Metadata.Key.of(key, Metadata.ASCII_STRING_MARSHALLER);
        for (String value : metadata.get(key)) {
          headers.put(headerKey, value);
        }
      }
    }
    return headers;
  }
}
