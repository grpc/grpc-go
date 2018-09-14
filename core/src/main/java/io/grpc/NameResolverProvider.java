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

package io.grpc;

import com.google.common.annotations.VisibleForTesting;
import java.net.URI;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Provider of name resolvers for name agnostic consumption.
 *
 * <p>Implementations <em>should not</em> throw. If they do, it may interrupt class loading. If
 * exceptions may reasonably occur for implementation-specific reasons, implementations should
 * generally handle the exception gracefully and return {@code false} from {@link #isAvailable()}.
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/4159")
public abstract class NameResolverProvider extends NameResolver.Factory {

  private static final Logger logger = Logger.getLogger(NameResolverProvider.class.getName());

  /**
   * The port number used in case the target or the underlying naming system doesn't provide a
   * port number.
   *
   * @since 1.0.0
   */
  @SuppressWarnings("unused") // Avoids outside callers accidentally depending on the super class.
  public static final Attributes.Key<Integer> PARAMS_DEFAULT_PORT =
      NameResolver.Factory.PARAMS_DEFAULT_PORT;

  @VisibleForTesting
  static final Iterable<Class<?>> HARDCODED_CLASSES = getHardCodedClasses();

  private static final List<NameResolverProvider> providers = ServiceProviders.loadAll(
      NameResolverProvider.class,
      HARDCODED_CLASSES,
      NameResolverProvider.class.getClassLoader(),
      new NameResolverPriorityAccessor());

  private static final NameResolver.Factory factory = new NameResolverFactory(providers);

  /**
   * Returns non-{@code null} ClassLoader-wide providers, in preference order.
   *
   * @since 1.0.0
   */
  public static List<NameResolverProvider> providers() {
    return providers;
  }

  /**
   * @since 1.0.0
   */
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
   *
   * @since 1.0.0
   */
  protected abstract boolean isAvailable();

  /**
   * A priority, from 0 to 10 that this provider should be used, taking the current environment into
   * consideration. 5 should be considered the default, and then tweaked based on environment
   * detection. A priority of 0 does not imply that the provider wouldn't work; just that it should
   * be last in line.
   *
   * @since 1.0.0
   */
  protected abstract int priority();

  private static final class NameResolverFactory extends NameResolver.Factory {
    private final List<NameResolverProvider> providers;

    NameResolverFactory(List<NameResolverProvider> providers) {
      this.providers = Collections.unmodifiableList(new ArrayList<>(providers));
    }

    @Override
    @Nullable
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
      if (providers.isEmpty()) {
        String msg = "No NameResolverProviders found via ServiceLoader, including for DNS. "
            + "This is probably due to a broken build. If using ProGuard, check your configuration";
        throw new RuntimeException(msg);
      }
    }
  }

  @VisibleForTesting
  static final List<Class<?>> getHardCodedClasses() {
    // Class.forName(String) is used to remove the need for ProGuard configuration. Note that
    // ProGuard does not detect usages of Class.forName(String, boolean, ClassLoader):
    // https://sourceforge.net/p/proguard/bugs/418/
    try {
      return Collections.<Class<?>>singletonList(
          Class.forName("io.grpc.internal.DnsNameResolverProvider"));
    } catch (ClassNotFoundException e) {
      logger.log(Level.FINE, "Unable to find DNS NameResolver", e);
    }
    return Collections.emptyList();
  }

  private static final class NameResolverPriorityAccessor
      implements ServiceProviders.PriorityAccessor<NameResolverProvider> {

    NameResolverPriorityAccessor() {}

    @Override
    public boolean isAvailable(NameResolverProvider provider) {
      return provider.isAvailable();
    }

    @Override
    public int getPriority(NameResolverProvider provider) {
      return provider.priority();
    }
  }
}
