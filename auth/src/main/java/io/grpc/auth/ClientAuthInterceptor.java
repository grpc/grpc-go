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

import com.google.auth.Credentials;
import com.google.common.base.Preconditions;

import io.grpc.Call;
import io.grpc.Channel;
import io.grpc.ClientInterceptor;
import io.grpc.ClientInterceptors.ForwardingCall;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;

import java.io.IOException;
import java.util.List;
import java.util.Map;
import java.util.concurrent.Executor;

/**
 * Client interceptor that authenticates all calls by binding header data provided by a credential.
 * Typically this will populate the Authorization header but other headers may also be filled out.
 *
 * <p> Uses the new and simplified Google auth library:
 * https://github.com/google/google-auth-library-java
 */
public class ClientAuthInterceptor implements ClientInterceptor {

  private final Credentials credentials;

  private Metadata.Headers cached;
  private Map<String, List<String>> lastMetadata;

  public ClientAuthInterceptor(Credentials credentials, Executor executor) {
    this.credentials = Preconditions.checkNotNull(credentials);
  }

  @Override
  public <ReqT, RespT> Call<ReqT, RespT> interceptCall(MethodDescriptor<ReqT, RespT> method,
                                                       Channel next) {
    // TODO(ejona86): If the call fails for Auth reasons, this does not properly propagate info that
    // would be in WWW-Authenticate, because it does not yet have access to the header.
    return new ForwardingCall<ReqT, RespT>(next.newCall(method)) {
      @Override
      public void start(Listener<RespT> responseListener, Metadata.Headers headers) {
        try {
          synchronized (this) {
            // TODO(lryan): This is icky but the current auth library stores the same
            // metadata map until the next refresh cycle. This will be fixed once
            // https://github.com/google/google-auth-library-java/issues/3
            // is resolved.
            if (lastMetadata == null || lastMetadata != credentials.getRequestMetadata()) {
              lastMetadata = credentials.getRequestMetadata();
              cached = toHeaders(lastMetadata);
            }
          }
          headers.merge(cached);
          super.start(responseListener, headers);
        } catch (IOException ioe) {
          responseListener.onClose(Status.fromThrowable(ioe), new Metadata.Trailers());
        }
      }
    };
  }

  private static final Metadata.Headers toHeaders(Map<String, List<String>> metadata) {
    Metadata.Headers headers = new Metadata.Headers();
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