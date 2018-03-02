/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

package io.grpc;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import java.net.URI;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;

/**
 * Provider of name resolvers for name agnostic consumption.
 *
 * <p>Implementations <em>should not</em> throw. If they do, it may interrupt class loading. If
 * exceptions may reasonably occur for implementation-specific reasons, implementations should
 * generally handle the exception gracefully and return {@code false} from {@link #isAvailable()}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4159")
public abstract class NameResolverProvider extends NameResolver.Factory {
  /**
   * The port number used in case the target or the underlying naming system doesn't provide a
   * port number.
   */
  public static final Attributes.Key<Integer> PARAMS_DEFAULT_PORT =
      NameResolver.Factory.PARAMS_DEFAULT_PORT;
  @VisibleForTesting
  static final Iterable<Class<?>> HARDCODED_CLASSES = new HardcodedClasses();

  private static final List<NameResolverProvider> providers = ServiceProviders.loadAll(
      NameResolverProvider.class,
      HARDCODED_CLASSES,
      NameResolverProvider.class.getClassLoader(),
      new ServiceProviders.PriorityAccessor<NameResolverProvider>() {
        @Override
        public boolean isAvailable(NameResolverProvider provider) {
          return provider.isAvailable();
        }

        @Override
        public int getPriority(NameResolverProvider provider) {
          return provider.priority();
        }
      });

  private static final NameResolver.Factory factory = new NameResolverFactory(providers);

  /**
   * Returns non-{@code null} ClassLoader-wide providers, in preference order.
   */
  public static List<NameResolverProvider> providers() {
    return providers;
  }

  public static NameResolver.Factory asFactory() {
    return factory;
  }

  @VisibleForTesting
  static NameResolver.Factory asFactory(List<NameResolverProvider> providers) {
    return new NameResolverFactory(providers);
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

  private static class NameResolverFactory extends NameResolver.Factory {
    private final List<NameResolverProvider> providers;

    public NameResolverFactory(List<NameResolverProvider> providers) {
      this.providers = providers;
    }

    @Override
    public NameResolver newNameResolver(URI targetUri, Attributes params) {
      checkForProviders();
      for (NameResolverProvider provider : providers) {
        NameResolver resolver = provider.newNameResolver(targetUri, params);
        if (resolver != null) {
          return resolver;
        }
      }
      return null;
    }

    @Override
    public String getDefaultScheme() {
      checkForProviders();
      return providers.get(0).getDefaultScheme();
    }

    private void checkForProviders() {
      Preconditions.checkState(!providers.isEmpty(),
          "No NameResolverProviders found via ServiceLoader, including for DNS. "
          + "This is probably due to a broken build. If using ProGuard, check your configuration");
    }
  }

  @VisibleForTesting
  static final class HardcodedClasses implements Iterable<Class<?>> {
    @Override
    public Iterator<Class<?>> iterator() {
      List<Class<?>> list = new ArrayList<Class<?>>();
      // Class.forName(String) is used to remove the need for ProGuard configuration. Note that
      // ProGuard does not detect usages of Class.forName(String, boolean, ClassLoader):
      // https://sourceforge.net/p/proguard/bugs/418/
      try {
        list.add(Class.forName("io.grpc.internal.DnsNameResolverProvider"));
      } catch (ClassNotFoundException e) {
        // ignore
      }
      return list.iterator();
    }
  }
}
