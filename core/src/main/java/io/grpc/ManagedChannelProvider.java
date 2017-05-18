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
      = load(getCorrectClassLoader());

  @VisibleForTesting
  static ManagedChannelProvider load(ClassLoader classLoader) {
    Iterable<ManagedChannelProvider> candidates;
    if (isAndroid()) {
      candidates = getCandidatesViaHardCoded(classLoader);
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
    return ServiceLoader.load(ManagedChannelProvider.class, classLoader);
  }

  /**
   * Load providers from a hard-coded list. This avoids using getResource(), which has performance
   * problems on Android (see https://github.com/grpc/grpc-java/issues/2037). Any provider that may
   * be used on Android is free to be added here.
   */
  @VisibleForTesting
  public static Iterable<ManagedChannelProvider> getCandidatesViaHardCoded(
      ClassLoader classLoader) {
    List<ManagedChannelProvider> list = new ArrayList<ManagedChannelProvider>();
    try {
      list.add(create(Class.forName("io.grpc.okhttp.OkHttpChannelProvider", true, classLoader)));
    } catch (ClassNotFoundException ex) {
      // ignore
    }
    try {
      list.add(create(Class.forName("io.grpc.netty.NettyChannelProvider", true, classLoader)));
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

  private static ClassLoader getCorrectClassLoader() {
    if (isAndroid()) {
      // When android:sharedUserId or android:process is used, Android will setup a dummy
      // ClassLoader for the thread context (http://stackoverflow.com/questions/13407006),
      // instead of letting users to manually set context class loader, we choose the
      // correct class loader here.
      return ManagedChannelProvider.class.getClassLoader();
    }
    return Thread.currentThread().getContextClassLoader();
  }

  /**
   * Returns whether current platform is Android.
   */
  protected static boolean isAndroid() {
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
