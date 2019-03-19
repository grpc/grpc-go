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

package io.grpc.internal;

import static org.junit.Assert.assertSame;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.ArgumentMatchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.CallOptions;
import io.grpc.ConnectivityState;
import io.grpc.ForwardingTestUtil;
import io.grpc.ManagedChannel;
import io.grpc.MethodDescriptor;
import io.grpc.testing.TestMethodDescriptors;
import java.lang.reflect.Method;
import java.util.Collections;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class ForwardingManagedChannelTest {
  private final ManagedChannel mock = mock(ManagedChannel.class);
  private final ForwardingManagedChannel forward = new ForwardingManagedChannel(mock) {};

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ManagedChannel.class,
        mock,
        forward,
        Collections.<Method>emptyList());
  }

  @Test
  public void shutdown() {
    ManagedChannel ret = mock(ManagedChannel.class);
    when(mock.shutdown()).thenReturn(ret);
    assertSame(ret, forward.shutdown());
  }

  @Test
  public void isShutdown() {
    when(mock.isShutdown()).thenReturn(true);
    assertSame(true, forward.isShutdown());
  }

  @Test
  public void isTerminated() {
    when(mock.isTerminated()).thenReturn(true);
    assertSame(true, forward.isTerminated());
  }

  @Test
  public void shutdownNow() {
    ManagedChannel ret = mock(ManagedChannel.class);
    when(mock.shutdownNow()).thenReturn(ret);
    assertSame(ret, forward.shutdownNow());
  }

  @Test
  public void awaitTermination() throws Exception {
    long timeout = 1234;
    TimeUnit unit = TimeUnit.MILLISECONDS;
    forward.awaitTermination(timeout, unit);
    verify(mock).awaitTermination(eq(timeout), eq(unit));
  }

  @Test
  public void newCall() {
    NoopClientCall<Void, Void> clientCall = new NoopClientCall<>();
    CallOptions callOptions = CallOptions.DEFAULT.withoutWaitForReady();
    MethodDescriptor<Void, Void> method = TestMethodDescriptors.voidMethod();
    when(mock.newCall(same(method), same(callOptions))).thenReturn(clientCall);
    assertSame(clientCall, forward.newCall(method, callOptions));
  }

  @Test
  public void authority() {
    String authority = "authority5678";
    when(mock.authority()).thenReturn(authority);
    assertSame(authority, forward.authority());
  }

  @Test
  public void getState() {
    when(mock.getState(true)).thenReturn(ConnectivityState.READY);
    assertSame(ConnectivityState.READY, forward.getState(true));
  }

  @Test
  public void notifyWhenStateChanged() {
    Runnable callback = new Runnable() {
      @Override
      public void run() { }
    };
    forward.notifyWhenStateChanged(ConnectivityState.READY, callback);
    verify(mock).notifyWhenStateChanged(same(ConnectivityState.READY), same(callback));
  }
}
