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

import com.google.common.annotations.VisibleForTesting;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.Context.CancellationListener;
import io.grpc.Context;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.health.v1.HealthCheckRequest;
import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.health.v1.HealthCheckResponse.ServingStatus;
import io.grpc.health.v1.HealthGrpc;
import io.grpc.stub.StreamObserver;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.concurrent.ConcurrentHashMap;
import javax.annotation.Nullable;
import javax.annotation.concurrent.GuardedBy;

final class HealthServiceImpl extends HealthGrpc.HealthImplBase {

  // Due to the latency of rpc calls, synchronization of the map does not help with consistency.
  // However, need use ConcurrentHashMap to allow concurrent reading by check().
  private final ConcurrentHashMap<String, ServingStatus> statusMap =
      new ConcurrentHashMap<String, ServingStatus>();

  private final Object watchLock = new Object();

  // Technically a Multimap<String, StreamObserver<HealthCheckResponse>>.  The Boolean value is not
  // used.  The StreamObservers need to be kept in a identity-equality set, to make sure
  // user-defined equals() doesn't confuse our book-keeping of the StreamObservers.  Constructing
  // such Multimap would require extra lines and the end result is not significally simpler, thus I
  // would rather not have the Guava collections dependency.
  @GuardedBy("watchLock")
  private final HashMap<String, IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean>>
      watchers =
          new HashMap<String, IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean>>();

  @Override
  public void check(HealthCheckRequest request,
      StreamObserver<HealthCheckResponse> responseObserver) {
    ServingStatus status = statusMap.get(request.getService());
    if (status == null) {
      responseObserver.onError(new StatusException(
          Status.NOT_FOUND.withDescription("unknown service " + request.getService())));
    } else {
      HealthCheckResponse response = HealthCheckResponse.newBuilder().setStatus(status).build();
      responseObserver.onNext(response);
      responseObserver.onCompleted();
    }
  }

  @Override
  public void watch(HealthCheckRequest request,
      final StreamObserver<HealthCheckResponse> responseObserver) {
    final String service = request.getService();
    synchronized (watchLock) {
      ServingStatus status = statusMap.get(service);
      responseObserver.onNext(getResponseForWatch(status));
      IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean> serviceWatchers =
          watchers.get(service);
      if (serviceWatchers == null) {
        serviceWatchers = new IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean>();
        watchers.put(service, serviceWatchers);
      }
      serviceWatchers.put(responseObserver, Boolean.TRUE);
    }
    Context.current().addListener(
        new CancellationListener() {
          @Override
          // Called when the client has closed the stream
          public void cancelled(Context context) {
            synchronized (watchLock) {
              IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean> serviceWatchers =
                  watchers.get(service);
              if (serviceWatchers != null) {
                serviceWatchers.remove(responseObserver);
                if (serviceWatchers.isEmpty()) {
                  watchers.remove(service);
                }
              }
            }
          }
        },
        MoreExecutors.directExecutor());
  }

  void setStatus(String service, ServingStatus status) {
    synchronized (watchLock) {
      ServingStatus prevStatus = statusMap.put(service, status);
      if (prevStatus != status) {
        notifyWatchers(service, status);
      }
    }
  }

  void clearStatus(String service) {
    synchronized (watchLock) {
      ServingStatus prevStatus = statusMap.remove(service);
      if (prevStatus != null) {
        notifyWatchers(service, null);
      }
    }
  }

  @VisibleForTesting
  int numWatchersForTest(String service) {
    synchronized (watchLock) {
      IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean> serviceWatchers =
          watchers.get(service);
      if (serviceWatchers == null) {
        return 0;
      }
      return serviceWatchers.size();
    }
  }

  @GuardedBy("watchLock")
  private void notifyWatchers(String service, @Nullable ServingStatus status) {
    HealthCheckResponse response = getResponseForWatch(status);
    IdentityHashMap<StreamObserver<HealthCheckResponse>, Boolean> serviceWatchers =
        watchers.get(service);
    if (serviceWatchers != null) {
      for (StreamObserver<HealthCheckResponse> responseObserver : serviceWatchers.keySet()) {
        responseObserver.onNext(response);
      }
    }
  }

  private static HealthCheckResponse getResponseForWatch(@Nullable ServingStatus recordedStatus) {
    return HealthCheckResponse.newBuilder().setStatus(
        recordedStatus == null ? ServingStatus.SERVICE_UNKNOWN : recordedStatus).build();
  }
}
