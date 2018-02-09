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

import io.grpc.ForwardingServerCall.SimpleForwardingServerCall;
import io.grpc.MethodDescriptor.MethodType;
import java.lang.reflect.Method;
import java.util.Collections;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Unit tests for {@link ForwardingServerCall}.
 */
@RunWith(JUnit4.class)
public class ForwardingServerCallTest {
  @Mock
  private ServerCall<Integer, String> mock;
  private ForwardingServerCall<Integer, String> forwarder;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    forwarder = new SimpleForwardingServerCall<Integer, String>(mock) {};
  }

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ServerCall.class, mock, forwarder, Collections.<Method>emptyList());
  }

  @Test
  public void testRequest() {
    forwarder.request(1234);
    verify(mock).request(1234);
  }

  @Test
  public void testSendHeaders() {
    Metadata headers = new Metadata();
    forwarder.sendHeaders(headers);
    verify(mock).sendHeaders(same(headers));
  }

  @Test
  public void testSendMessage() {
    String message = "hello";
    forwarder.sendMessage(message);
    verify(mock).sendMessage(message);
  }

  @Test
  public void testIsReady() {
    when(mock.isReady()).thenReturn(true);
    assertEquals(true, forwarder.isReady());
  }

  @Test
  public void testClose() {
    Status status = Status.INTERNAL;
    Metadata trailers = new Metadata();
    forwarder.close(status, trailers);
    verify(mock).close(same(status), same(trailers));
  }

  @Test
  public void testIsCancelled() {
    when(mock.isCancelled()).thenReturn(true);
    assertEquals(true, forwarder.isCancelled());
  }

  @Test
  public void testSetMessageCompression() {
    forwarder.setMessageCompression(true);
    verify(mock).setMessageCompression(true);
  }

  @Test
  public void testSetCompression() {
    forwarder.setCompression("mycompressor");
    verify(mock).setCompression("mycompressor");
  }

  @Test
  public void testGetAttributes() {
    Attributes attributes = Attributes.newBuilder().build();
    when(mock.getAttributes()).thenReturn(attributes);
    assertSame(attributes, forwarder.getAttributes());
  }

  @Test
  public void testGetAuthority() {
    when(mock.getAuthority()).thenReturn("myauthority");
    assertEquals("myauthority", forwarder.getAuthority());
  }

  @Test
  public void testGetMethodDescriptor() {
    MethodDescriptor<Integer, String> method = MethodDescriptor.<Integer, String>newBuilder()
        .setFullMethodName("test/method")
        .setType(MethodType.UNARY)
        .setRequestMarshaller(IntegerMarshaller.INSTANCE)
        .setResponseMarshaller(StringMarshaller.INSTANCE)
        .build();
    when(mock.getMethodDescriptor()).thenReturn(method);
    assertSame(method, forwarder.getMethodDescriptor());
  }
}
