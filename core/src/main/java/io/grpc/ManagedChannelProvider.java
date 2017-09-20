/*
 * Copyright 2015, gRPC Authors All rights reserved.
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
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.List;
import java.util.ServiceConfigurationError;
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
      = load(ManagedChannelProvider.class.getClassLoader());

  @VisibleForTesting
  static ManagedChannelProvider load(ClassLoader classLoader) {
    Iterable<ManagedChannelProvider> candidates;
    if (isAndroid()) {
      candidates = getCandidatesViaHardCoded();
    } else {
      candidates = getCandidatesViaServiceLoader(classLoader);
    }
    List<ManagedChannelProvider> list = new ArrayList<ManagedChannelProvider>();
    for (ManagedChannelProvider current : candidates) {
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
   * Loads service providers for the {@link ManagedChannelProvider} service using
   * {@link ServiceLoader}.
   */
  @VisibleForTesting
  public static Iterable<ManagedChannelProvider> getCandidatesViaServiceLoader(
      ClassLoader classLoader) {
    Iterable<ManagedChannelProvider> i
        = ServiceLoader.load(ManagedChannelProvider.class, classLoader);
    // Attempt to load using the context class loader and ServiceLoader.
    // This allows frameworks like http://aries.apache.org/modules/spi-fly.html to plug in.
    if (!i.iterator().hasNext()) {
      i = ServiceLoader.load(ManagedChannelProvider.class);
    }
    return i;
  }

  /**
   * Load providers from a hard-coded list. This avoids using getResource(), which has performance
   * problems on Android (see https://github.com/grpc/grpc-java/issues/2037). Any provider that may
   * be used on Android is free to be added here.
   */
  @VisibleForTesting
  public static Iterable<ManagedChannelProvider> getCandidatesViaHardCoded() {
    // Class.forName(String) is used to remove the need for ProGuard configuration. Note that
    // ProGuard does not detect usages of Class.forName(String, boolean, ClassLoader):
    // https://sourceforge.net/p/proguard/bugs/418/
    List<ManagedChannelProvider> list = new ArrayList<ManagedChannelProvider>();
    try {
      list.add(create(Class.forName("io.grpc.okhttp.OkHttpChannelProvider")));
    } catch (ClassNotFoundException ex) {
      // ignore
    }
    try {
      list.add(create(Class.forName("io.grpc.netty.NettyChannelProvider")));
    } catch (ClassNotFoundException ex) {
      // ignore
    }
    return list;
  }

  @VisibleForTesting
  static ManagedChannelProvider create(Class<?> rawClass) {
    try {
      return rawClass.asSubclass(ManagedChannelProvider.class).getConstructor().newInstance();
    } catch (Throwable t) {
      throw new ServiceConfigurationError(
          "Provider " + rawClass.getName() + " could not be instantiated: " + t, t);
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
   * Returns whether current platform is Android.
   */
  protected static boolean isAndroid() {
    try {
      // Specify a class loader instead of null because we may be running under Robolectric
      Class.forName("android.app.Application", /*initialize=*/ false,
          ManagedChannelProvider.class.getClassLoader());
      return true;
    } catch (Exception e) {
      // If Application isn't loaded, it might as well not be Android.
      return false;
    }
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

  /**
   * Creates a new builder with the given target URI.
   */
  protected abstract ManagedChannelBuilder<?> builderForTarget(String target);

  /**
   * Thrown when no suitable {@link ManagedChannelProvider} objects can be found.
   */
  public static final class ProviderNotFoundException extends RuntimeException {
    private static final long serialVersionUID = 1;

    public ProviderNotFoundException(String msg) {
      super(msg);
    }
  }
}
