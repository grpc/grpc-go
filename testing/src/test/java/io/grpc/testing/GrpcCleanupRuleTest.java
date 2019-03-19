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

package io.grpc.testing;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.AdditionalAnswers.delegatesTo;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyLong;
import static org.mockito.Mockito.doReturn;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.inOrder;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.verifyNoMoreInteractions;

import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.internal.FakeClock;
import io.grpc.testing.GrpcCleanupRule.Resource;
import java.util.concurrent.TimeUnit;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.junit.runners.model.MultipleFailureException;
import org.junit.runners.model.Statement;
import org.mockito.InOrder;

/**
 * Unit tests for {@link GrpcCleanupRule}.
 */
@RunWith(JUnit4.class)
public class GrpcCleanupRuleTest {
  public static final FakeClock fakeClock = new FakeClock();

  @Rule
  public ExpectedException thrown = ExpectedException.none();

  @Test
  public void registerChannelReturnSameChannel() {
    ManagedChannel channel = mock(ManagedChannel.class);
    assertSame(channel, new GrpcCleanupRule().register(channel));
  }

  @Test
  public void registerServerReturnSameServer() {
    Server server = mock(Server.class);
    assertSame(server, new GrpcCleanupRule().register(server));
  }

  @Test
  public void registerNullChannelThrowsNpe() {
    ManagedChannel channel = null;
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    thrown.expect(NullPointerException.class);
    thrown.expectMessage("channel");

    grpcCleanup.register(channel);
  }

  @Test
  public void registerNullServerThrowsNpe() {
    Server server = null;
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    thrown.expect(NullPointerException.class);
    thrown.expectMessage("server");

    grpcCleanup.register(server);
  }

  @Test
  public void singleChannelCleanup() throws Throwable {
    // setup
    ManagedChannel channel = mock(ManagedChannel.class);
    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, channel);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(channel);

    boolean awaitTerminationFailed = false;
    try {
      // will throw because channel.awaitTermination(long, TimeUnit) will return false;
      grpcCleanup.apply(statement, null /* description*/).evaluate();
    } catch (AssertionError e) {
      awaitTerminationFailed = true;
    }

    // verify
    assertTrue(awaitTerminationFailed);
    inOrder.verify(statement).evaluate();
    inOrder.verify(channel).shutdown();
    inOrder.verify(channel).awaitTermination(anyLong(), any(TimeUnit.class));
    inOrder.verify(channel).shutdownNow();
  }

  @Test
  public void singleServerCleanup() throws Throwable {
    // setup
    Server server = mock(Server.class);
    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, server);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(server);

    boolean awaitTerminationFailed = false;
    try {
      // will throw because channel.awaitTermination(long, TimeUnit) will return false;
      grpcCleanup.apply(statement, null /* description*/).evaluate();
    } catch (AssertionError e) {
      awaitTerminationFailed = true;
    }

    // verify
    assertTrue(awaitTerminationFailed);
    inOrder.verify(statement).evaluate();
    inOrder.verify(server).shutdown();
    inOrder.verify(server).awaitTermination(anyLong(), any(TimeUnit.class));
    inOrder.verify(server).shutdownNow();
  }

  @Test
  public void multiResource_cleanupGracefully() throws Throwable {
    // setup
    Resource resource1 = mock(Resource.class);
    Resource resource2 = mock(Resource.class);
    Resource resource3 = mock(Resource.class);
    doReturn(true).when(resource1).awaitReleased(anyLong(), any(TimeUnit.class));
    doReturn(true).when(resource2).awaitReleased(anyLong(), any(TimeUnit.class));
    doReturn(true).when(resource3).awaitReleased(anyLong(), any(TimeUnit.class));

    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, resource1, resource2, resource3);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(resource1);
    grpcCleanup.register(resource2);
    grpcCleanup.register(resource3);
    grpcCleanup.apply(statement, null /* description*/).evaluate();

    // Verify.
    inOrder.verify(statement).evaluate();

    inOrder.verify(resource3).cleanUp();
    inOrder.verify(resource2).cleanUp();
    inOrder.verify(resource1).cleanUp();

    inOrder.verify(resource3).awaitReleased(anyLong(), any(TimeUnit.class));
    inOrder.verify(resource2).awaitReleased(anyLong(), any(TimeUnit.class));
    inOrder.verify(resource1).awaitReleased(anyLong(), any(TimeUnit.class));

    inOrder.verifyNoMoreInteractions();

    verify(resource1, never()).forceCleanUp();
    verify(resource2, never()).forceCleanUp();
    verify(resource3, never()).forceCleanUp();
  }

  @Test
  public void baseTestFails() throws Throwable {
    // setup
    Resource resource = mock(Resource.class);

    Statement statement = mock(Statement.class);
    doThrow(new Exception()).when(statement).evaluate();

    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(resource);

    boolean baseTestFailed = false;
    try {
      grpcCleanup.apply(statement, null /* description*/).evaluate();
    } catch (Exception e) {
      baseTestFailed = true;
    }

    // verify
    assertTrue(baseTestFailed);

    verify(resource).forceCleanUp();
    verifyNoMoreInteractions(resource);

    verify(resource, never()).cleanUp();
    verify(resource, never()).awaitReleased(anyLong(), any(TimeUnit.class));
  }

  @Test
  public void multiResource_awaitReleasedFails() throws Throwable {
    // setup
    Resource resource1 = mock(Resource.class);
    Resource resource2 = mock(Resource.class);
    Resource resource3 = mock(Resource.class);
    doReturn(true).when(resource1).awaitReleased(anyLong(), any(TimeUnit.class));
    doReturn(false).when(resource2).awaitReleased(anyLong(), any(TimeUnit.class));
    doReturn(true).when(resource3).awaitReleased(anyLong(), any(TimeUnit.class));

    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, resource1, resource2, resource3);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(resource1);
    grpcCleanup.register(resource2);
    grpcCleanup.register(resource3);

    boolean cleanupFailed = false;
    try {
      grpcCleanup.apply(statement, null /* description*/).evaluate();
    } catch (AssertionError e) {
      cleanupFailed = true;
    }

    // verify
    assertTrue(cleanupFailed);

    inOrder.verify(statement).evaluate();

    inOrder.verify(resource3).cleanUp();
    inOrder.verify(resource2).cleanUp();
    inOrder.verify(resource1).cleanUp();

    inOrder.verify(resource3).awaitReleased(anyLong(), any(TimeUnit.class));
    inOrder.verify(resource2).awaitReleased(anyLong(), any(TimeUnit.class));
    inOrder.verify(resource2).forceCleanUp();
    inOrder.verify(resource1).forceCleanUp();

    inOrder.verifyNoMoreInteractions();

    verify(resource3, never()).forceCleanUp();
    verify(resource1, never()).awaitReleased(anyLong(), any(TimeUnit.class));
  }

  @Test
  public void multiResource_awaitReleasedInterrupted() throws Throwable {
    // setup
    Resource resource1 = mock(Resource.class);
    Resource resource2 = mock(Resource.class);
    Resource resource3 = mock(Resource.class);
    doReturn(true).when(resource1).awaitReleased(anyLong(), any(TimeUnit.class));
    doThrow(new InterruptedException())
        .when(resource2).awaitReleased(anyLong(), any(TimeUnit.class));
    doReturn(true).when(resource3).awaitReleased(anyLong(), any(TimeUnit.class));

    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, resource1, resource2, resource3);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(resource1);
    grpcCleanup.register(resource2);
    grpcCleanup.register(resource3);

    boolean cleanupFailed = false;
    try {
      grpcCleanup.apply(statement, null /* description*/).evaluate();
    } catch (InterruptedException e) {
      cleanupFailed = true;
    }

    // verify
    assertTrue(cleanupFailed);
    assertTrue(Thread.interrupted());

    inOrder.verify(statement).evaluate();

    inOrder.verify(resource3).cleanUp();
    inOrder.verify(resource2).cleanUp();
    inOrder.verify(resource1).cleanUp();

    inOrder.verify(resource3).awaitReleased(anyLong(), any(TimeUnit.class));
    inOrder.verify(resource2).awaitReleased(anyLong(), any(TimeUnit.class));
    inOrder.verify(resource2).forceCleanUp();
    inOrder.verify(resource1).forceCleanUp();

    inOrder.verifyNoMoreInteractions();

    verify(resource3, never()).forceCleanUp();
    verify(resource1, never()).awaitReleased(anyLong(), any(TimeUnit.class));
  }

  @Test
  public void multiResource_timeoutCalculation() throws Throwable {
    // setup

    Resource resource1 = mock(FakeResource.class,
        delegatesTo(new FakeResource(1 /* cleanupNanos */, 10 /* awaitReleaseNanos */)));

    Resource resource2 = mock(FakeResource.class,
        delegatesTo(new FakeResource(100 /* cleanupNanos */, 1000 /* awaitReleaseNanos */)));


    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, resource1, resource2);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule().setTicker(fakeClock.getTicker());

    // run
    grpcCleanup.register(resource1);
    grpcCleanup.register(resource2);
    grpcCleanup.apply(statement, null /* description*/).evaluate();

    // verify
    inOrder.verify(statement).evaluate();

    inOrder.verify(resource2).cleanUp();
    inOrder.verify(resource1).cleanUp();

    inOrder.verify(resource2).awaitReleased(
        TimeUnit.SECONDS.toNanos(10) - 100 - 1, TimeUnit.NANOSECONDS);
    inOrder.verify(resource1).awaitReleased(
        TimeUnit.SECONDS.toNanos(10) - 100 - 1 - 1000, TimeUnit.NANOSECONDS);

    inOrder.verifyNoMoreInteractions();

    verify(resource2, never()).forceCleanUp();
    verify(resource1, never()).forceCleanUp();
  }

  @Test
  public void multiResource_timeoutCalculation_customTimeout() throws Throwable {
    // setup

    Resource resource1 = mock(FakeResource.class,
        delegatesTo(new FakeResource(1 /* cleanupNanos */, 10 /* awaitReleaseNanos */)));

    Resource resource2 = mock(FakeResource.class,
        delegatesTo(new FakeResource(100 /* cleanupNanos */, 1000 /* awaitReleaseNanos */)));


    Statement statement = mock(Statement.class);
    InOrder inOrder = inOrder(statement, resource1, resource2);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule()
        .setTicker(fakeClock.getTicker()).setTimeout(3000, TimeUnit.NANOSECONDS);

    // run
    grpcCleanup.register(resource1);
    grpcCleanup.register(resource2);
    grpcCleanup.apply(statement, null /* description*/).evaluate();

    // verify
    inOrder.verify(statement).evaluate();

    inOrder.verify(resource2).cleanUp();
    inOrder.verify(resource1).cleanUp();

    inOrder.verify(resource2).awaitReleased(3000 - 100 - 1, TimeUnit.NANOSECONDS);
    inOrder.verify(resource1).awaitReleased(3000 - 100 - 1 - 1000, TimeUnit.NANOSECONDS);

    inOrder.verifyNoMoreInteractions();

    verify(resource2, never()).forceCleanUp();
    verify(resource1, never()).forceCleanUp();
  }

  @Test
  public void baseTestFailsThenCleanupFails() throws Throwable {
    // setup
    Exception baseTestFailure = new Exception();

    Statement statement = mock(Statement.class);
    doThrow(baseTestFailure).when(statement).evaluate();

    Resource resource1 = mock(Resource.class);
    Resource resource2 = mock(Resource.class);
    Resource resource3 = mock(Resource.class);
    doThrow(new RuntimeException()).when(resource2).forceCleanUp();

    InOrder inOrder = inOrder(statement, resource1, resource2, resource3);
    GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();

    // run
    grpcCleanup.register(resource1);
    grpcCleanup.register(resource2);
    grpcCleanup.register(resource3);

    Throwable failure = null;
    try {
      grpcCleanup.apply(statement, null /* description*/).evaluate();
    } catch (Throwable e) {
      failure = e;
    }

    // verify
    assertThat(failure).isInstanceOf(MultipleFailureException.class);
    assertSame(baseTestFailure, ((MultipleFailureException) failure).getFailures().get(0));

    inOrder.verify(statement).evaluate();
    inOrder.verify(resource3).forceCleanUp();
    inOrder.verify(resource2).forceCleanUp();
    inOrder.verifyNoMoreInteractions();

    verify(resource1, never()).cleanUp();
    verify(resource2, never()).cleanUp();
    verify(resource3, never()).cleanUp();
    verify(resource1, never()).forceCleanUp();
  }

  public static class FakeResource implements Resource {
    private final long cleanupNanos;
    private final long awaitReleaseNanos;

    private FakeResource(long cleanupNanos, long awaitReleaseNanos) {
      this.cleanupNanos = cleanupNanos;
      this.awaitReleaseNanos = awaitReleaseNanos;
    }

    @Override
    public void cleanUp() {
      fakeClock.forwardTime(cleanupNanos, TimeUnit.NANOSECONDS);
    }

    @Override
    public void forceCleanUp() {
    }

    @Override
    public boolean awaitReleased(long duration, TimeUnit timeUnit) {
      fakeClock.forwardTime(awaitReleaseNanos, TimeUnit.NANOSECONDS);
      return true;
    }
  }
}
