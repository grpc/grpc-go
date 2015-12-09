/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc;

import com.google.common.base.Preconditions;

import java.net.URI;
import java.util.concurrent.CopyOnWriteArrayList;

import javax.annotation.concurrent.ThreadSafe;

/**
 * A registry that holds various {@link NameResolver.Factory}s and dispatches target URI to the
 * first one that can handle it.
 */
@ExperimentalApi
@ThreadSafe
public final class NameResolverRegistry extends NameResolver.Factory {
  private static final NameResolverRegistry defaultRegistry =
      new NameResolverRegistry(DnsNameResolverFactory.getInstance());

  private final CopyOnWriteArrayList<NameResolver.Factory> registry =
      new CopyOnWriteArrayList<NameResolver.Factory>();

  public static NameResolverRegistry getDefaultRegistry() {
    return defaultRegistry;
  }

  private final String defaultScheme;

  private NameResolverRegistry(NameResolver.Factory defaultResolverFactory) {
    register(defaultResolverFactory);
    defaultScheme = Preconditions.checkNotNull(
        defaultResolverFactory.getDefaultScheme(), "defaultScheme");
  }

  /**
   * Registers a {@link NameResolver.Factory}.
   */
  public void register(NameResolver.Factory factory) {
    registry.add(0, factory);
  }

  /**
   * Returns a {@link NameResolver} created by the first factory that can handle the given target
   * URI, or {@code null} if no one can handle it.
   *
   * <p>The factory that was registered later has higher priority.
   */
  @Override
  public NameResolver newNameResolver(URI targetUri, Attributes params) {
    for (NameResolver.Factory factory : registry) {
      NameResolver resolver = factory.newNameResolver(targetUri, params);
      if (resolver != null) {
        return resolver;
      }
    }
    return null;
  }

  @Override
  public String getDefaultScheme() {
    return defaultScheme;
  }
}
