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

import static junit.framework.TestCase.assertSame;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.ClientCall.Listener;
import io.grpc.ForwardingClientCall.SimpleForwardingClientCall;
import java.lang.reflect.Method;
import java.util.Collections;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link ForwardingClientCall}.
 */
@RunWith(JUnit4.class)
public class ForwardingClientCallTest {
  @Mock
  private ClientCall<Integer, String> mock;
  private ForwardingClientCall<Integer, String> forwarder;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    forwarder = new SimpleForwardingClientCall<Integer, String>(mock) {};
  }

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ClientCall.class, mock, forwarder, Collections.<Method>emptyList());
  }

  @Test
  public void testRequest() {
    forwarder.request(1234);
    verify(mock).request(1234);
  }

  @Test
  public void testStart() {
    Metadata headers = new Metadata();
    Listener<String> listener = new Listener<String>() {};
    forwarder.start(listener, headers);
    verify(mock).start(same(listener), same(headers));
  }

  @Test
  public void testSendMessage() {
    int message = 3456;
    forwarder.sendMessage(message);
    verify(mock).sendMessage(message);
  }

  @Test
  public void testIsReady() {
    when(mock.isReady()).thenReturn(true);
    assertEquals(true, forwarder.isReady());
  }

  @Test
  public void testSetMessageCompression() {
    forwarder.setMessageCompression(true);
    verify(mock).setMessageCompression(true);
  }

  @Test
  public void testGetAttributes() {
    Attributes attributes = Attributes.newBuilder().build();
    when(mock.getAttributes()).thenReturn(attributes);
    assertSame(attributes, forwarder.getAttributes());
  }
}
