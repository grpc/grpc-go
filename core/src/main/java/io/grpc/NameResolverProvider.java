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

package io.grpc;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.base.Preconditions;
import java.net.URI;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.List;
import java.util.ServiceConfigurationError;
import java.util.ServiceLoader;

/**
 * Provider of name resolvers for name agnostic consumption.
 *
 * <p>Implementations <em>should not</em> throw. If they do, it may interrupt class loading. If
 * exceptions may reasonably occur for implementation-specific reasons, implementations should
 * generally handle the exception gracefully and return {@code false} from {@link #isAvailable()}.
 */
@Internal
public abstract class NameResolverProvider extends NameResolver.Factory {
  /**
   * The port number used in case the target or the underlying naming system doesn't provide a
   * port number.
   */
  public static final Attributes.Key<Integer> PARAMS_DEFAULT_PORT =
      NameResolver.Factory.PARAMS_DEFAULT_PORT;

  private static final List<NameResolverProvider> providers
      = load(getCorrectClassLoader());
  private static final NameResolver.Factory factory = new NameResolverFactory(providers);

  @VisibleForTesting
  static List<NameResolverProvider> load(ClassLoader classLoader) {
    Iterable<NameResolverProvider> candidates;
    if (isAndroid()) {
      candidates = getCandidatesViaHardCoded(classLoader);
    } else {
      candidates = getCandidatesViaServiceLoader(classLoader);
    }
    List<NameResolverProvider> list = new ArrayList<NameResolverProvider>();
    for (NameResolverProvider current : candidates) {
      if (!current.isAvailable()) {
        continue;
      }
      list.add(current);
    }
    // Sort descending based on priority.
    Collections.sort(list, Collections.reverseOrder(new Comparator<NameResolverProvider>() {
      @Override
      public int compare(NameResolverProvider f1, NameResolverProvider f2) {
        return f1.priority() - f2.priority();
      }
    }));
    return Collections.unmodifiableList(list);
  }

  @VisibleForTesting
  public static Iterable<NameResolverProvider> getCandidatesViaServiceLoader(
      ClassLoader classLoader) {
    return ServiceLoader.load(NameResolverProvider.class, classLoader);
  }

  /**
   * Load providers from a hard-coded list. This avoids using getResource(), which has performance
   * problems on Android (see https://github.com/grpc/grpc-java/issues/2037). Any provider that may
   * be used on Android is free to be added here.
   */
  @VisibleForTesting
  public static Iterable<NameResolverProvider> getCandidatesViaHardCoded(ClassLoader classLoader) {
    List<NameResolverProvider> list = new ArrayList<NameResolverProvider>();
    try {
      list.add(create(
          Class.forName("io.grpc.internal.DnsNameResolverProvider", true, classLoader)));
    } catch (ClassNotFoundException ex) {
      // ignore
    }
    return list;
  }

  @VisibleForTesting
  static NameResolverProvider create(Class<?> rawClass) {
    try {
      return rawClass.asSubclass(NameResolverProvider.class).getConstructor().newInstance();
    } catch (Throwable t) {
      throw new ServiceConfigurationError(
          "Provider " + rawClass.getName() + " could not be instantiated: " + t, t);
    }
  }

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

  private static ClassLoader getCorrectClassLoader() {
    if (isAndroid()) {
      // When android:sharedUserId or android:process is used, Android will setup a dummy
      // ClassLoader for the thread context (http://stackoverflow.com/questions/13407006),
      // instead of letting users to manually set context class loader, we choose the
      // correct class loader here.
      return NameResolverProvider.class.getClassLoader();
    }
    return Thread.currentThread().getContextClassLoader();
  }

  private static boolean isAndroid() {
    try {
      Class.forName("android.app.Application", /*initialize=*/ false, null);
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
}
