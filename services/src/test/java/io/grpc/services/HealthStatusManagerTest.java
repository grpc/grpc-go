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

import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;

import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.health.v1.HealthCheckRequest;
import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.health.v1.HealthGrpc;
import io.grpc.stub.StreamObserver;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.InOrder;

/** Unit tests for {@link HealthStatusManager}. */
@RunWith(JUnit4.class)
public class HealthStatusManagerTest {

  private final HealthStatusManager manager = new HealthStatusManager();
  private final HealthGrpc.HealthImplBase health =
      (HealthGrpc.HealthImplBase) manager.getHealthService();
  private final HealthCheckResponse.ServingStatus status
      = HealthCheckResponse.ServingStatus.UNKNOWN;

  @Test
  public void getHealthService_getterReturnsTheSameHealthRefAfterUpdate() throws Exception {
    manager.setStatus("", status);
    assertEquals(health, manager.getHealthService());
  }


  @Test
  public void checkValidStatus() throws Exception {
    //setup
    manager.setStatus("", status);
    HealthCheckRequest request = HealthCheckRequest.newBuilder().setService("").build();
    @SuppressWarnings("unchecked")
    StreamObserver<HealthCheckResponse> observer = mock(StreamObserver.class);

    //test
    health.check(request, observer);

    //verify
    InOrder inOrder = inOrder(observer);
    inOrder.verify(observer, times(1)).onNext(any(HealthCheckResponse.class));
    inOrder.verify(observer, times(1)).onCompleted();
    verify(observer, never()).onError(any(Throwable.class));
  }

  @Test
  public void checkStatusNotFound() throws Exception {
    //setup
    manager.setStatus("", status);
    HealthCheckRequest request
        = HealthCheckRequest.newBuilder().setService("invalid").build();
    @SuppressWarnings("unchecked")
    StreamObserver<HealthCheckResponse> observer = mock(StreamObserver.class);

    //test
    health.check(request, observer);

    //verify
    ArgumentCaptor<StatusException> exception = ArgumentCaptor.forClass(StatusException.class);
    verify(observer, times(1)).onError(exception.capture());
    assertEquals(Status.Code.NOT_FOUND, exception.getValue().getStatus().getCode());

    verify(observer, never()).onCompleted();
  }

  @Test
  public void notFoundForClearedStatus() throws Exception {
    //setup
    manager.setStatus("", status);
    manager.clearStatus("");
    HealthCheckRequest request
        = HealthCheckRequest.newBuilder().setService("").build();
    @SuppressWarnings("unchecked")
    StreamObserver<HealthCheckResponse> observer = mock(StreamObserver.class);

    //test
    health.check(request, observer);

    //verify
    ArgumentCaptor<StatusException> exception = ArgumentCaptor.forClass(StatusException.class);
    verify(observer, times(1)).onError(exception.capture());
    assertEquals(Status.Code.NOT_FOUND, exception.getValue().getStatus().getCode());

    verify(observer, never()).onCompleted();
  }
}
