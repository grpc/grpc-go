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

package io.grpc.internal;

import static com.google.common.base.MoreObjects.firstNonNull;
import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.Attributes;
import io.grpc.CallCredentials;
import io.grpc.CallOptions;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.SecurityLevel;
import java.net.SocketAddress;
import java.util.concurrent.Executor;
import javax.annotation.Nullable;

final class CallCredentialsApplyingTransportFactory implements ClientTransportFactory {
  private final ClientTransportFactory delegate;
  private final Executor appExecutor;

  CallCredentialsApplyingTransportFactory(
      ClientTransportFactory delegate, Executor appExecutor) {
    this.delegate = checkNotNull(delegate, "delegate");
    this.appExecutor = checkNotNull(appExecutor, "appExecutor");
  }

  @Override
  public ConnectionClientTransport newClientTransport(
      SocketAddress serverAddress, String authority, @Nullable String userAgent) {
    return new CallCredentialsApplyingTransport(
        delegate.newClientTransport(serverAddress, authority, userAgent), authority);
  }

  @Override
  public void close() {
    delegate.close();
  }

  private class CallCredentialsApplyingTransport extends ForwardingConnectionClientTransport {
    private final ConnectionClientTransport delegate;
    private final String authority;

    CallCredentialsApplyingTransport(ConnectionClientTransport delegate, String authority) {
      this.delegate = checkNotNull(delegate, "delegate");
      this.authority = checkNotNull(authority, "authority");
    }

    @Override
    protected ConnectionClientTransport delegate() {
      return delegate;
    }

    @Override
    public ClientStream newStream(
        MethodDescriptor<?, ?> method, Metadata headers, CallOptions callOptions) {
      CallCredentials creds = callOptions.getCredentials();
      if (creds != null) {
        MetadataApplierImpl applier = new MetadataApplierImpl(
            delegate, method, headers, callOptions);
        Attributes.Builder effectiveAttrsBuilder = Attributes.newBuilder()
            .set(CallCredentials.ATTR_AUTHORITY, authority)
            .set(CallCredentials.ATTR_SECURITY_LEVEL, SecurityLevel.NONE)
            .setAll(delegate.getAttributes());
        if (callOptions.getAuthority() != null) {
          effectiveAttrsBuilder.set(CallCredentials.ATTR_AUTHORITY, callOptions.getAuthority());
        }
        creds.applyRequestMetadata(method, effectiveAttrsBuilder.build(),
            firstNonNull(callOptions.getExecutor(), appExecutor), applier);
        return applier.returnStream();
      } else {
        return delegate.newStream(method, headers, callOptions);
      }
    }
  }
}
