/*
 * Copyright 2015 The gRPC Authors
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
import io.grpc.ServiceProviders.PriorityAccessor;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;

/**
 * Provider of managed channels for transport agnostic consumption.
 *
 * <p>Implementations <em>should not</em> throw. If they do, it may interrupt class loading. If
 * exceptions may reasonably occur for implementation-specific reasons, implementations should
 * generally handle the exception gracefully and return {@code false} from {@link #isAvailable()}.
 */
@Internal
public abstract class ManagedChannelProvider {
  @VisibleForTesting
  static final Iterable<Class<?>> HARDCODED_CLASSES = new HardcodedClasses();

  private static final ManagedChannelProvider provider = ServiceProviders.load(
      ManagedChannelProvider.class,
      HARDCODED_CLASSES,
      ManagedChannelProvider.class.getClassLoader(),
      new PriorityAccessor<ManagedChannelProvider>() {
        @Override
        public boolean isAvailable(ManagedChannelProvider provider) {
          return provider.isAvailable();
        }

        @Override
        public int getPriority(ManagedChannelProvider provider) {
          return provider.priority();
        }
      });

  /**
   * Returns the ClassLoader-wide default channel.
   *
   * @throws ProviderNotFoundException if no provider is available
   */
  public static ManagedChannelProvider provider() {
    if (provider == null) {
      throw new ProviderNotFoundException("No functional channel service provider found. "
          + "Try adding a dependency on the grpc-okhttp, grpc-netty, or grpc-netty-shaded "
          + "artifact");
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

  private static final class HardcodedClasses implements Iterable<Class<?>> {
    @Override
    public Iterator<Class<?>> iterator() {
      List<Class<?>> list = new ArrayList<>();
      try {
        list.add(Class.forName("io.grpc.okhttp.OkHttpChannelProvider"));
      } catch (ClassNotFoundException ex) {
        // ignore
      }
      try {
        list.add(Class.forName("io.grpc.netty.NettyChannelProvider"));
      } catch (ClassNotFoundException ex) {
        // ignore
      }
      return list.iterator();
    }
  }
}
