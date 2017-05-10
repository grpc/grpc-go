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
    assertEquals(Status.NOT_FOUND, exception.getValue().getStatus());

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
    assertEquals(Status.NOT_FOUND, exception.getValue().getStatus());

    verify(observer, never()).onCompleted();
  }
}
