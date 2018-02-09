/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

package io.grpc;

import static org.mockito.Matchers.same;
import static org.mockito.Mockito.verify;

import io.grpc.ForwardingClientCallListener.SimpleForwardingClientCallListener;
import java.lang.reflect.Method;
import java.util.Collections;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link ForwardingClientCallListener}.
 */
@RunWith(JUnit4.class)
public class ForwardingClientCallListenerTest {
  @Mock private ClientCall.Listener<Integer> mock;
  private ForwardingClientCallListener<Integer> forwarder;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    forwarder = new SimpleForwardingClientCallListener<Integer>(mock) {};
  }

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ClientCall.Listener.class,
        mock,
        forwarder,
        Collections.<Method>emptyList());
  }

  @Test
  public void testOnHeaders() {
    Metadata headers = new Metadata();
    forwarder.onHeaders(headers);
    verify(mock).onHeaders(same(headers));
  }

  @Test
  public void onMessage() {
    forwarder.onMessage(12345);
    verify(mock).onMessage(12345);
  }

  @Test
  public void close() {
    Status status = Status.INTERNAL;
    Metadata trailers = new Metadata();
    forwarder.onClose(status, trailers);
    verify(mock).onClose(same(status), same(trailers));
  }
}
