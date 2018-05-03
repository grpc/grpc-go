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

import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.health.v1.HealthCheckRequest;
import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.health.v1.HealthCheckResponse.ServingStatus;
import io.grpc.health.v1.HealthGrpc;
import io.grpc.stub.StreamObserver;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import javax.annotation.Nullable;


final class HealthServiceImpl extends HealthGrpc.HealthImplBase {

  /* Due to the latency of rpc calls, synchronization of the map does not help with consistency.
   * However, need use ConcurrentHashMap to prevent the possible race condition of concurrently
   * putting two keys with a colliding hashCode into the map.*/
  private final Map<String, ServingStatus> statusMap
      = new ConcurrentHashMap<String, ServingStatus>();

  @Override
  public void check(HealthCheckRequest request,
      StreamObserver<HealthCheckResponse> responseObserver) {
    ServingStatus status = getStatus(request.getService());
    if (status == null) {
      responseObserver.onError(new StatusException(
          Status.NOT_FOUND.withDescription("unknown service " + request.getService())));
    } else {
      HealthCheckResponse response = HealthCheckResponse.newBuilder().setStatus(status).build();
      responseObserver.onNext(response);
      responseObserver.onCompleted();
    }
  }

  void setStatus(String service, ServingStatus status) {
    statusMap.put(service, status);
  }

  @Nullable
  ServingStatus getStatus(String service) {
    return statusMap.get(service);
  }

  void clearStatus(String service) {
    statusMap.remove(service);
  }
}
