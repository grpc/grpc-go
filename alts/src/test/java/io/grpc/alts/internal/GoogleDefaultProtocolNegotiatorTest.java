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

package io.grpc.alts.internal;

import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.internal.GrpcAttributes;
import io.grpc.netty.GrpcHttp2ConnectionHandler;
import io.grpc.netty.ProtocolNegotiator;
import io.grpc.netty.ProtocolNegotiator.Handler;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public final class GoogleDefaultProtocolNegotiatorTest {
  private ProtocolNegotiator altsProtocolNegotiator;
  private ProtocolNegotiator tlsProtocolNegotiator;
  private GoogleDefaultProtocolNegotiator googleProtocolNegotiator;

  @Before
  public void setUp() {
    altsProtocolNegotiator = mock(ProtocolNegotiator.class);
    tlsProtocolNegotiator = mock(ProtocolNegotiator.class);
    googleProtocolNegotiator =
        new GoogleDefaultProtocolNegotiator(altsProtocolNegotiator, tlsProtocolNegotiator);
  }

  @Test
  public void altsHandler() {
    Attributes eagAttributes =
        Attributes.newBuilder().set(GrpcAttributes.ATTR_LB_PROVIDED_BACKEND, true).build();
    GrpcHttp2ConnectionHandler mockHandler = mock(GrpcHttp2ConnectionHandler.class);
    when(mockHandler.getEagAttributes()).thenReturn(eagAttributes);
    Handler handler = googleProtocolNegotiator.newHandler(mockHandler);
    verify(altsProtocolNegotiator, times(1)).newHandler(mockHandler);
    verify(tlsProtocolNegotiator, never()).newHandler(mockHandler);
  }

  @Test
  public void tlsHandler() {
    Attributes eagAttributes = Attributes.EMPTY;
    GrpcHttp2ConnectionHandler mockHandler = mock(GrpcHttp2ConnectionHandler.class);
    when(mockHandler.getEagAttributes()).thenReturn(eagAttributes);
    Handler handler = googleProtocolNegotiator.newHandler(mockHandler);
    verify(altsProtocolNegotiator, never()).newHandler(mockHandler);
    verify(tlsProtocolNegotiator, times(1)).newHandler(mockHandler);
  }
}
