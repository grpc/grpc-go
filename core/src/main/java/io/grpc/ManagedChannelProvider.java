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

import com.google.common.annotations.VisibleForTesting;

import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.List;
import java.util.ServiceLoader;

/**
 * Provider of managed channels for transport agnostic consumption.
 *
 * <p>Implementations <em>should not</em> throw. If they do, it may interrupt class loading. If
 * exceptions may reasonably occur for implementation-specific reasons, implementations should
 * generally handle the exception gracefully and return {@code false} from {@link #isAvailable()}.
 */
@Internal
public abstract class ManagedChannelProvider {
  private static final ManagedChannelProvider provider
      = load(Thread.currentThread().getContextClassLoader());

  @VisibleForTesting
  static ManagedChannelProvider load(ClassLoader classLoader) {
    ServiceLoader<ManagedChannelProvider> providers
        = ServiceLoader.load(ManagedChannelProvider.class, classLoader);
    List<ManagedChannelProvider> list = new ArrayList<ManagedChannelProvider>();
    for (ManagedChannelProvider current : providers) {
      if (!current.isAvailable()) {
        continue;
      }
      list.add(current);
    }
    if (list.isEmpty()) {
      return null;
    } else {
      return Collections.max(list, new Comparator<ManagedChannelProvider>() {
        @Override
        public int compare(ManagedChannelProvider f1, ManagedChannelProvider f2) {
          return f1.priority() - f2.priority();
        }
      });
    }
  }

  /**
   * Returns the ClassLoader-wide default channel.
   *
   * @throws ProviderNotFoundException if no provider is available
   */
  public static ManagedChannelProvider provider() {
    if (provider == null) {
      throw new ProviderNotFoundException("No functional channel service provider found. "
          + "Try adding a dependency on the grpc-okhttp or grpc-netty artifact");
    }
    return provider;
  }

  /**
   * Whether this provider is available for use, taking the current environment into consideration.
   * If {@code false}, no other methods are safe to be called.
   */
  protected abstract boolean isAvailable();

  /**
   * A priority, from 0 to 10 that this provider should be used, taking the current environment into
   * consideration. 5 should be considered the default, and then tweaked based on environment
   * detection. A priority of 0 does not imply that the provider wouldn't work; just that it should
   * be last in line.
   */
  protected abstract int priority();

  /**
   * Creates a new builder with the given host and port.
   */
  protected abstract ManagedChannelBuilder<?> builderForAddress(String name, int port);

  public static final class ProviderNotFoundException extends RuntimeException {
    public ProviderNotFoundException(String msg) {
      super(msg);
    }
  }
}
