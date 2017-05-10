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
@io.grpc.ExperimentalApi
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
