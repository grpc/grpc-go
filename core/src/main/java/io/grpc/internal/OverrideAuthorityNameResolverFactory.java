/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.internal;

import io.grpc.Attributes;
import io.grpc.NameResolver;
import java.net.URI;
import javax.annotation.Nullable;

/**
 * A wrapper class that overrides the authority of a NameResolver, while preserving all other
 * functionality.
 */
final class OverrideAuthorityNameResolverFactory extends NameResolver.Factory {
  private final NameResolver.Factory delegate;
  private final String authorityOverride;

  /**
   * Constructor for the {@link NameResolver.Factory}
   *
   * @param delegate The actual underlying factory that will produce the a {@link NameResolver}
   * @param authorityOverride The authority that will be returned by {@link
   *   NameResolver#getServiceAuthority()}
   */
  OverrideAuthorityNameResolverFactory(NameResolver.Factory delegate, String authorityOverride) {
    this.delegate = delegate;
    this.authorityOverride = authorityOverride;
  }

  @Nullable
  @Override
  public NameResolver newNameResolver(URI targetUri, Attributes params) {
    final NameResolver resolver = delegate.newNameResolver(targetUri, params);
    // Do not wrap null values. We do not want to impede error signaling.
    if (resolver == null) {
      return null;
    }
    return new ForwardingNameResolver(resolver) {
      @Override
      public String getServiceAuthority() {
        return authorityOverride;
      }
    };
  }

  @Override
  public String getDefaultScheme() {
    return delegate.getDefaultScheme();
  }
}
