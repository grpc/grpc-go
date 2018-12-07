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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.fail;

import io.grpc.BindableService;
import io.grpc.Context.CancellableContext;
import io.grpc.Context;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.health.v1.HealthCheckRequest;
import io.grpc.health.v1.HealthCheckResponse;
import io.grpc.health.v1.HealthCheckResponse.ServingStatus;
import io.grpc.health.v1.HealthGrpc;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcServerRule;
import java.util.ArrayDeque;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Tests for {@link HealthStatusManager}. */
@RunWith(JUnit4.class)
public class HealthStatusManagerTest {
  private static final String SERVICE1 = "service1";
  private static final String SERVICE2 = "service2";

  @Rule public final GrpcServerRule grpcServerRule = new GrpcServerRule().directExecutor();

  private final HealthStatusManager manager = new HealthStatusManager();
  private final HealthServiceImpl service = (HealthServiceImpl) manager.getHealthService();
  private HealthGrpc.HealthStub stub;
  private HealthGrpc.HealthBlockingStub blockingStub;

  @Before
  public void setup() {
    grpcServerRule.getServiceRegistry().addService(service);
    stub = HealthGrpc.newStub(grpcServerRule.getChannel());
    blockingStub = HealthGrpc.newBlockingStub(grpcServerRule.getChannel());
  }

  @After
  public void teardown() {
    // Health-check streams are usually not closed in the tests.  Force closing for clean up.
    grpcServerRule.getServer().shutdownNow();
  }

  @Test
  public void enterTerminalState_check() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    RespObserver obs = new RespObserver();
    service.check(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(2);
    HealthCheckResponse resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.SERVING);

    manager.enterTerminalState();
    obs = new RespObserver();
    service.check(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(2);
    resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.NOT_SERVING);

    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    obs = new RespObserver();
    service.check(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(2);
    resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.NOT_SERVING);
  }

  @Test
  public void enterTerminalState_watch() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    RespObserver obs = new RespObserver();
    service.watch(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(1);
    HealthCheckResponse resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.SERVING);
    obs.responses.clear();

    manager.enterTerminalState();
    assertThat(obs.responses).hasSize(1);
    resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.NOT_SERVING);
    obs.responses.clear();

    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    assertThat(obs.responses).isEmpty();
  }

  @Test
  public void enterTerminalState_ignoreClear() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    RespObserver obs = new RespObserver();
    service.check(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(2);
    HealthCheckResponse resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.SERVING);

    manager.enterTerminalState();
    obs = new RespObserver();
    service.check(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(2);
    resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.NOT_SERVING);

    manager.clearStatus(SERVICE1);
    obs = new RespObserver();
    service.check(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), obs);
    assertThat(obs.responses).hasSize(2);
    resp = (HealthCheckResponse) obs.responses.poll();
    assertThat(resp.getStatus()).isEqualTo(ServingStatus.NOT_SERVING);
  }

  @Test
  public void getHealthService_getterReturnsTheSameHealthRefAfterUpdate() throws Exception {
    BindableService health = manager.getHealthService();
    manager.setStatus(SERVICE1, ServingStatus.UNKNOWN);
    assertThat(health).isSameAs(manager.getHealthService());
  }

  @Test
  public void checkValidStatus() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.NOT_SERVING);
    manager.setStatus(SERVICE2, ServingStatus.SERVING);
    HealthCheckRequest request = HealthCheckRequest.newBuilder().setService(SERVICE1).build();
    HealthCheckResponse response =
        blockingStub.withDeadlineAfter(5, TimeUnit.SECONDS).check(request);
    assertThat(response).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.NOT_SERVING).build());

    request = HealthCheckRequest.newBuilder().setService(SERVICE2).build();
    response = blockingStub.withDeadlineAfter(5, TimeUnit.SECONDS).check(request);
    assertThat(response).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVING).build());
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(0);
    assertThat(service.numWatchersForTest(SERVICE2)).isEqualTo(0);
  }

  @Test
  public void checkStatusNotFound() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    // SERVICE2's status is not set
    HealthCheckRequest request
        = HealthCheckRequest.newBuilder().setService(SERVICE2).build();
    try {
      blockingStub.withDeadlineAfter(5, TimeUnit.SECONDS).check(request);
      fail("Should've failed");
    } catch (StatusRuntimeException e) {
      assertThat(e.getStatus().getCode()).isEqualTo(Status.Code.NOT_FOUND);
    }
    assertThat(service.numWatchersForTest(SERVICE2)).isEqualTo(0);
  }

  @Test
  public void notFoundForClearedStatus() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    manager.clearStatus(SERVICE1);
    HealthCheckRequest request
        = HealthCheckRequest.newBuilder().setService(SERVICE1).build();
    try {
      blockingStub.withDeadlineAfter(5, TimeUnit.SECONDS).check(request);
      fail("Should've failed");
    } catch (StatusRuntimeException e) {
      assertThat(e.getStatus().getCode()).isEqualTo(Status.Code.NOT_FOUND);
    }
  }

  @Test
  public void watch() throws Exception {
    manager.setStatus(SERVICE1, ServingStatus.UNKNOWN);

    // Start a watch on SERVICE1
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(0);
    RespObserver respObs1 = new RespObserver();
    stub.watch(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), respObs1);
    // Will get the current status
    assertThat(respObs1.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.UNKNOWN).build());        
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(1);

    // Status change is notified of to the RPC
    manager.setStatus(SERVICE1, ServingStatus.SERVING);
    assertThat(respObs1.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVING).build());        

    // Start another watch on SERVICE1
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(1);
    RespObserver respObs1b = new RespObserver();
    stub.watch(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), respObs1b);
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(2);
    // Will get the current status
    assertThat(respObs1b.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVING).build());

    // Start a watch on SERVICE2, which is not known yet
    assertThat(service.numWatchersForTest(SERVICE2)).isEqualTo(0);
    RespObserver respObs2 = new RespObserver();
    stub.watch(HealthCheckRequest.newBuilder().setService(SERVICE2).build(), respObs2);
    assertThat(service.numWatchersForTest(SERVICE2)).isEqualTo(1);
    // Current status is SERVICE_UNKNOWN
    assertThat(respObs2.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVICE_UNKNOWN).build());

    // Set status for SERVICE2, which will be notified of
    manager.setStatus(SERVICE2, ServingStatus.NOT_SERVING);
    assertThat(respObs2.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.NOT_SERVING).build());

    // Clear the status for SERVICE1, which will be notified of
    manager.clearStatus(SERVICE1);
    assertThat(respObs1.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVICE_UNKNOWN).build());
    assertThat(respObs1b.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVICE_UNKNOWN).build());

    // All responses have been accounted for
    assertThat(respObs1.responses).isEmpty();
    assertThat(respObs1b.responses).isEmpty();
    assertThat(respObs2.responses).isEmpty();
  }

  @Test
  public void watchRemovedWhenClientCloses() throws Exception {
    CancellableContext withCancellation = Context.current().withCancellation();
    Context prevCtx = withCancellation.attach();
    RespObserver respObs1 = new RespObserver();
    try {
      assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(0);
      stub.watch(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), respObs1);
    } finally {
      withCancellation.detach(prevCtx);
    }
    RespObserver respObs1b = new RespObserver();
    stub.watch(HealthCheckRequest.newBuilder().setService(SERVICE1).build(), respObs1b);
    RespObserver respObs2 = new RespObserver();
    stub.watch(HealthCheckRequest.newBuilder().setService(SERVICE2).build(), respObs2);

    assertThat(respObs1.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVICE_UNKNOWN).build());
    assertThat(respObs1b.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVICE_UNKNOWN).build());
    assertThat(respObs2.responses.poll()).isEqualTo(
        HealthCheckResponse.newBuilder().setStatus(ServingStatus.SERVICE_UNKNOWN).build());
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(2);
    assertThat(service.numWatchersForTest(SERVICE2)).isEqualTo(1);
    assertThat(respObs1.responses).isEmpty();
    assertThat(respObs1b.responses).isEmpty();
    assertThat(respObs2.responses).isEmpty();

    // This will cancel the RPC with respObs1
    withCancellation.close();

    assertThat(respObs1.responses.poll()).isInstanceOf(Throwable.class);
    assertThat(service.numWatchersForTest(SERVICE1)).isEqualTo(1);
    assertThat(service.numWatchersForTest(SERVICE2)).isEqualTo(1);
    assertThat(respObs1.responses).isEmpty();
    assertThat(respObs1b.responses).isEmpty();
    assertThat(respObs2.responses).isEmpty();
  }

  private static class RespObserver implements StreamObserver<HealthCheckResponse> {
    final ArrayDeque<Object> responses = new ArrayDeque<Object>();

    @Override
    public void onNext(HealthCheckResponse value) {
      responses.add(value);
    }

    @Override
    public void onError(Throwable t) {
      responses.add(t);
    }

    @Override
    public void onCompleted() {
      responses.add("onCompleted");
    }
  }
}
