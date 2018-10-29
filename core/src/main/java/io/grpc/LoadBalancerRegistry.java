/*
 * Copyright 2018 The gRPC Authors
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

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.logging.Level;
import java.util.logging.Logger;
import javax.annotation.Nullable;

/**
 * Registry of {@link LoadBalancerProvider}s.  The {@link #getDefaultRegistry default instance}
 * loads providers at runtime through the Java service provider mechanism.
 *
 * @since 1.17.0
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1771")
public final class LoadBalancerRegistry {
  private static final Logger logger = Logger.getLogger(LoadBalancerRegistry.class.getName());
  private static LoadBalancerRegistry instance;
  private static final Iterable<Class<?>> HARDCODED_CLASSES = getHardCodedClasses();

  private final Map<String, LoadBalancerProvider> providers;

  @VisibleForTesting
  LoadBalancerRegistry(List<LoadBalancerProvider> providerList) {
    // Use LinkedHashMap to preserve order s othat it's easier to test
    LinkedHashMap<String, LoadBalancerProvider> providerMap = new LinkedHashMap<>();
    for (LoadBalancerProvider provider : providerList) {
      if (!provider.isAvailable()) {
        logger.fine(provider + " found but not available");
        continue;
      }
      String policy = provider.getPolicyName();
      LoadBalancerProvider existing = providerMap.get(policy);
      if (existing == null) {
        logger.fine("Found " + provider);
        providerMap.put(policy, provider);
      } else {
        if (existing.getPriority() < provider.getPriority()) {
          logger.fine(provider + " overrides " + existing + " because of higher priority");
          providerMap.put(policy, provider);
        } else if (existing.getPriority() > provider.getPriority()) {
          logger.fine(provider + " doesn't override " + existing + " because of lower priority");
        } else {
          LoadBalancerProvider selected = existing;
          if (existing.getClass().getName().compareTo(provider.getClass().getName()) < 0) {
            providerMap.put(policy, provider);
            selected = provider;
          }
          logger.warning(
              provider + " and " + existing + " has the same priority. "
              + selected + " is selected for this time. "
              + "You should make them differ in either policy name or priority, or remove "
              + "one of them from your classpath");
        }
      }
    }
    providers = Collections.unmodifiableMap(providerMap);
  }

  /**
   * Returns the default registry that loads providers via the Java service loader mechanism.
   */
  public static synchronized LoadBalancerRegistry getDefaultRegistry() {
    if (instance == null) {
      List<LoadBalancerProvider> providerList = ServiceProviders.loadAll(
          LoadBalancerProvider.class,
          HARDCODED_CLASSES,
          LoadBalancerProvider.class.getClassLoader(),
          new LoadBalancerPriorityAccessor());
      instance = new LoadBalancerRegistry(providerList);
    }
    return instance;
  }

  /**
   * Returns the provider for the given load-balancing policy, or {@code null} if no suitable
   * provider can be found.  Each provider declares its policy name via {@link
   * LoadBalancerProvider#getPolicyName}.
   */
  @Nullable
  public LoadBalancerProvider getProvider(String policy) {
    return providers.get(checkNotNull(policy, "policy"));
  }

  /**
   * Returns effective providers.
   */
  @VisibleForTesting
  Map<String, LoadBalancerProvider> providers() {
    return providers;
  }

  @VisibleForTesting
  static List<Class<?>> getHardCodedClasses() {
    // Class.forName(String) is used to remove the need for ProGuard configuration. Note that
    // ProGuard does not detect usages of Class.forName(String, boolean, ClassLoader):
    // https://sourceforge.net/p/proguard/bugs/418/
    try {
      return Collections.<Class<?>>singletonList(
          Class.forName("io.grpc.internal.PickFirstLoadBalancerProvider"));
    } catch (ClassNotFoundException e) {
      logger.log(Level.WARNING, "Unable to find pick-first LoadBalancer", e);
    }
    return Collections.emptyList();
  }

  private static final class LoadBalancerPriorityAccessor
      implements ServiceProviders.PriorityAccessor<LoadBalancerProvider> {

    LoadBalancerPriorityAccessor() {}

    @Override
    public boolean isAvailable(LoadBalancerProvider provider) {
      return provider.isAvailable();
    }

    @Override
    public int getPriority(LoadBalancerProvider provider) {
      return provider.getPriority();
    }
  }
}
