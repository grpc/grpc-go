/*
 * Copyright 2017, gRPC Authors All rights reserved.
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

import com.google.common.annotations.VisibleForTesting;
import io.grpc.ClientInterceptor;
import io.grpc.ServerInterceptor;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.Iterator;
import java.util.List;
import java.util.ServiceConfigurationError;
import java.util.ServiceLoader;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

public abstract class BinaryLogProvider {
  private static final Logger logger = Logger.getLogger(BinaryLogProvider.class.getName());
  private static final BinaryLogProvider NULL_PROVIDER = new NullProvider();
  private static final BinaryLogProvider PROVIDER = load(BinaryLogProvider.class.getClassLoader());

  /**
   * Always returns a {@code BinaryLogProvider}, even if the provider always returns null
   * interceptors.
   */
  public static BinaryLogProvider provider() {
    return PROVIDER;
  }

  @VisibleForTesting
  static BinaryLogProvider load(ClassLoader classLoader) {
    try {
      return loadHelper(classLoader);
    } catch (Throwable t) {
      logger.log(
          Level.SEVERE, "caught exception loading BinaryLogProvider, will disable binary log", t);
      return NULL_PROVIDER;
    }
  }

  private static BinaryLogProvider loadHelper(ClassLoader classLoader) {
    if (isAndroid()) {
      return NULL_PROVIDER;
    }

    Iterator<BinaryLogProvider> iter = getCandidatesViaServiceLoader(classLoader).iterator();
    List<BinaryLogProvider> list = new ArrayList<BinaryLogProvider>();
    while (iter.hasNext()) {
      // The iterator comes from ServiceLoader and may throw when next() is called
      try {
        list.add(iter.next());
      } catch (ServiceConfigurationError e) {
        logger.log(Level.SEVERE, "caught exception creating an instance of BinaryLogProvider", e);
      }
    }
    if (list.isEmpty()) {
      return NULL_PROVIDER;
    } else {
      return Collections.max(list, new Comparator<BinaryLogProvider>() {
        @Override
        public int compare(BinaryLogProvider f1, BinaryLogProvider f2) {
          return f1.priority() - f2.priority();
        }
      });
    }
  }

  private static ServiceLoader<BinaryLogProvider> getCandidatesViaServiceLoader(
      ClassLoader classLoader) {
    ServiceLoader<BinaryLogProvider> i = ServiceLoader.load(BinaryLogProvider.class, classLoader);
    // Attempt to load using the context class loader and ServiceLoader.
    // This allows frameworks like http://aries.apache.org/modules/spi-fly.html to plug in.
    if (!i.iterator().hasNext()) {
      i = ServiceLoader.load(BinaryLogProvider.class);
    }
    return i;
  }

  /**
   * Returns a {@link ServerInterceptor} for binary logging. gRPC is free to cache the interceptor,
   * so the interceptor must be reusable across server calls. At runtime, the request and response
   * types passed into the interceptor is always {@link java.io.InputStream}.
   * Returns {@code null} if this method is not binary logged.
   */
  @Nullable
  public abstract ServerInterceptor getServerInterceptor(String fullMethodName);

  /**
   * Returns a {@link ClientInterceptor} for binary logging. gRPC is free to cache the interceptor,
   * so the interceptor must be reusable across server calls. At runtime, the request and response
   * types passed into the interceptor is always {@link java.io.InputStream}.
   * Returns {@code null} if this method is not binary logged.
   */
  @Nullable
  public abstract ClientInterceptor getClientInterceptor(String fullMethodName);

  /**
   * A priority, from 0 to 10 that this provider should be used, taking the current environment into
   * consideration. 5 should be considered the default, and then tweaked based on environment
   * detection. A priority of 0 does not imply that the provider wouldn't work; just that it should
   * be last in line.
   */
  protected abstract int priority();

  /**
   * A provider that always returns null interceptors.
   */
  @VisibleForTesting
  static final class NullProvider extends BinaryLogProvider {
    @Nullable
    @Override
    public ServerInterceptor getServerInterceptor(String fullMethodName) {
      return null;
    }

    @Override
    public ClientInterceptor getClientInterceptor(String fullMethodName) {
      return null;
    }

    @Override
    protected int priority() {
      return 0;
    }
  }

  /**
   * Returns whether current platform is Android.
   */
  protected static boolean isAndroid() {
    try {
      // Specify a class loader instead of null because we may be running under Robolectric
      Class.forName("android.app.Application", /*initialize=*/ false,
          BinaryLogProvider.class.getClassLoader());
      return true;
    } catch (Exception e) {
      // If Application isn't loaded, it might as well not be Android.
      return false;
    }
  }
}
