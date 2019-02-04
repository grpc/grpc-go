/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc.util;

import io.grpc.BindableService;
import io.grpc.ExperimentalApi;
import io.grpc.HandlerRegistry;
import io.grpc.MethodDescriptor;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import javax.annotation.Nullable;
import javax.annotation.concurrent.ThreadSafe;

/**
 * Default implementation of {@link MutableHandlerRegistry}.
 *
 * <p>Uses {@link ConcurrentHashMap} to avoid service registration excessively
 * blocking method lookup.
 */
@ThreadSafe
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/933")
public final class MutableHandlerRegistry extends HandlerRegistry {
  private final ConcurrentMap<String, ServerServiceDefinition> services
      = new ConcurrentHashMap<>();

  /**
   * Registers a service.
   *
   * @return the previously registered service with the same service descriptor name if exists,
   *         otherwise {@code null}.
   */
  @Nullable
  public ServerServiceDefinition addService(ServerServiceDefinition service) {
    return services.put(service.getServiceDescriptor().getName(), service);
  }

  /**
   * Registers a service.
   *
   * @return the previously registered service with the same service descriptor name if exists,
   *         otherwise {@code null}.
   */
  @Nullable
  public ServerServiceDefinition addService(BindableService bindableService) {
    return addService(bindableService.bindService());
  }

  /**
   * Removes a registered service
   *
   * @return true if the service was found to be removed.
   */
  public boolean removeService(ServerServiceDefinition service) {
    return services.remove(service.getServiceDescriptor().getName(), service);
  }

  /**
   *  Note: This does not necessarily return a consistent view of the map.
   */
  @Override
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/2222")
  public List<ServerServiceDefinition> getServices() {
    return Collections.unmodifiableList(new ArrayList<>(services.values()));
  }

  /**
   * Note: This does not actually honor the authority provided.  It will, eventually in the future.
   */
  @Override
  @Nullable
  public ServerMethodDefinition<?, ?> lookupMethod(String methodName, @Nullable String authority) {
    String serviceName = MethodDescriptor.extractFullServiceName(methodName);
    if (serviceName == null) {
      return null;
    }
    ServerServiceDefinition service = services.get(serviceName);
    if (service == null) {
      return null;
    }
    return service.getMethod(methodName);
  }
}
