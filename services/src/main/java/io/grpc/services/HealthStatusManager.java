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

package io.grpc.services;

import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.BindableService;
import io.grpc.health.v1.HealthCheckResponse.ServingStatus;

/**
 * A {@code HealthStatusManager} object manages a health check service. A health check service is
 * created in the constructor of {@code HealthStatusManager}, and it can be retrieved by the
 * {@link #getHealthService()} method.
 * The health status manager can update the health statuses of the server.
 */
@io.grpc.ExperimentalApi("https://github.com/grpc/grpc-java/issues/4696")
public final class HealthStatusManager {

  private final HealthServiceImpl healthService;

  /**
   * Creates a new health service instance.
   */
  public HealthStatusManager() {
    healthService = new HealthServiceImpl();
  }

  /**
   * Gets the health check service created in the constructor.
   */
  public BindableService getHealthService() {
    return healthService;
  }

  /**
   * Updates the status of the server.
   * @param service the name of some aspect of the server that is associated with a health status.
   *     This name can have no relation with the gRPC services that the server is running with.
   *     It can also be an empty String {@code ""} per the gRPC specification.
   * @param status is one of the values {@link ServingStatus#SERVING},
   *     {@link ServingStatus#NOT_SERVING} and {@link ServingStatus#UNKNOWN}.
   */
  public void setStatus(String service, ServingStatus status) {
    checkNotNull(status, "status");
    healthService.setStatus(service, status);
  }

  /**
   * Clears the health status record of a service. The health service will respond with NOT_FOUND
   * error on checking the status of a cleared service.
   * @param service the name of some aspect of the server that is associated with a health status.
   *     This name can have no relation with the gRPC services that the server is running with.
   *     It can also be an empty String {@code ""} per the gRPC specification.
   */
  public void clearStatus(String service) {
    healthService.clearStatus(service);
  }
}
