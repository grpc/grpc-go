/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.util;

import static com.google.common.collect.Iterables.getOnlyElement;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;

import io.grpc.BindableService;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.ServerCallHandler;
import io.grpc.ServerMethodDefinition;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServiceDescriptor;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.MockitoAnnotations;

/** Unit tests for {@link MutableHandlerRegistry}. */
@RunWith(JUnit4.class)
public class MutableHandlerRegistryTest {
  private MutableHandlerRegistry registry = new MutableHandlerRegistry();

  @Mock
  private Marshaller<String> requestMarshaller;

  @Mock
  private Marshaller<Integer> responseMarshaller;

  @Mock
  private ServerCallHandler<String, Integer> flowHandler;

  @Mock
  private ServerCallHandler<String, Integer> coupleHandler;

  @Mock
  private ServerCallHandler<String, Integer> fewHandler;

  @Mock
  private ServerCallHandler<String, Integer>  otherFlowHandler;

  private ServerServiceDefinition basicServiceDefinition;
  private ServerServiceDefinition multiServiceDefinition;

  @SuppressWarnings("rawtypes")
  private ServerMethodDefinition flowMethodDefinition;

  @Before
  public void setUp() throws Exception {
    MockitoAnnotations.initMocks(this);
    MethodDescriptor<String, Integer> flowMethod = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodType.UNKNOWN)
        .setFullMethodName("basic/flow")
        .setRequestMarshaller(requestMarshaller)
        .setResponseMarshaller(responseMarshaller)
        .build();
    basicServiceDefinition = ServerServiceDefinition.builder(
        new ServiceDescriptor("basic", flowMethod))
        .addMethod(flowMethod, flowHandler)
        .build();

    MethodDescriptor<String, Integer> coupleMethod =
        flowMethod.toBuilder().setFullMethodName("multi/couple").build();
    MethodDescriptor<String, Integer> fewMethod =
        flowMethod.toBuilder().setFullMethodName("multi/few").build();
    multiServiceDefinition = ServerServiceDefinition.builder(
        new ServiceDescriptor("multi", coupleMethod, fewMethod))
        .addMethod(coupleMethod, coupleHandler)
        .addMethod(fewMethod, fewHandler)
        .build();

    flowMethodDefinition = getOnlyElement(basicServiceDefinition.getMethods());
  }

  /** Final checks for all tests. */
  @After
  public void makeSureMocksUnused() {
    Mockito.verifyZeroInteractions(requestMarshaller);
    Mockito.verifyZeroInteractions(responseMarshaller);
    Mockito.verifyNoMoreInteractions(flowHandler);
    Mockito.verifyNoMoreInteractions(coupleHandler);
    Mockito.verifyNoMoreInteractions(fewHandler);
  }

  @Test
  public void simpleLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    ServerMethodDefinition<?, ?> method = registry.lookupMethod("basic/flow");
    assertSame(flowMethodDefinition, method);

    assertNull(registry.lookupMethod("/basic/flow"));
    assertNull(registry.lookupMethod("basic/basic"));
    assertNull(registry.lookupMethod("flow/flow"));
    assertNull(registry.lookupMethod("completely/random"));
  }

  @Test
  public void simpleLookupWithBindable() {
    BindableService bindableService =
        new BindableService() {
          @Override
          public ServerServiceDefinition bindService() {
            return basicServiceDefinition;
          }
        };

    assertNull(registry.addService(bindableService));

    ServerMethodDefinition<?, ?> method = registry.lookupMethod("basic/flow");
    assertSame(flowMethodDefinition, method);
  }

  @Test
  public void multiServiceLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNull(registry.addService(multiServiceDefinition));

    ServerCallHandler<?, ?> handler = registry.lookupMethod("basic/flow").getServerCallHandler();
    assertSame(flowHandler, handler);
    handler = registry.lookupMethod("multi/couple").getServerCallHandler();
    assertSame(coupleHandler, handler);
    handler = registry.lookupMethod("multi/few").getServerCallHandler();
    assertSame(fewHandler, handler);
  }

  @Test
  public void removeAndLookup() {
    assertNull(registry.addService(multiServiceDefinition));
    assertNotNull(registry.lookupMethod("multi/couple"));
    assertNotNull(registry.lookupMethod("multi/few"));
    assertTrue(registry.removeService(multiServiceDefinition));
    assertNull(registry.lookupMethod("multi/couple"));
    assertNull(registry.lookupMethod("multi/few"));
  }

  @Test
  public void replaceAndLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNotNull(registry.lookupMethod("basic/flow"));
    MethodDescriptor<String, Integer> anotherMethod = MethodDescriptor.<String, Integer>newBuilder()
        .setType(MethodType.UNKNOWN)
        .setFullMethodName("basic/another")
        .setRequestMarshaller(requestMarshaller)
        .setResponseMarshaller(responseMarshaller)
        .build();
    ServerServiceDefinition replaceServiceDefinition = ServerServiceDefinition.builder(
        new ServiceDescriptor("basic", anotherMethod))
        .addMethod(anotherMethod, flowHandler).build();
    ServerMethodDefinition<?, ?> anotherMethodDefinition =
        replaceServiceDefinition.getMethod("basic/another");
    assertSame(basicServiceDefinition, registry.addService(replaceServiceDefinition));

    assertNull(registry.lookupMethod("basic/flow"));
    ServerMethodDefinition<?, ?> method = registry.lookupMethod("basic/another");
    assertSame(anotherMethodDefinition, method);
  }

  @Test
  public void removeSameSucceeds() {
    assertNull(registry.addService(basicServiceDefinition));
    assertTrue(registry.removeService(basicServiceDefinition));
  }

  @Test
  public void doubleRemoveFails() {
    assertNull(registry.addService(basicServiceDefinition));
    assertTrue(registry.removeService(basicServiceDefinition));
    assertFalse(registry.removeService(basicServiceDefinition));
  }

  @Test
  public void removeMissingFails() {
    assertFalse(registry.removeService(basicServiceDefinition));
  }

  @Test
  public void removeMissingNameConflictFails() {
    assertNull(registry.addService(basicServiceDefinition));
    assertFalse(registry.removeService(ServerServiceDefinition.builder(
        new ServiceDescriptor("basic")).build()));
  }

  @Test
  public void initialAddReturnsNull() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNull(registry.addService(multiServiceDefinition));
  }

  @Test
  public void missingMethodLookupReturnsNull() {
    assertNull(registry.lookupMethod("bad"));
  }

  @Test
  public void addAfterRemoveReturnsNull() {
    assertNull(registry.addService(basicServiceDefinition));
    assertTrue(registry.removeService(basicServiceDefinition));
    assertNull(registry.addService(basicServiceDefinition));
  }

  @Test
  public void addReturnsPrevious() {
    assertNull(registry.addService(basicServiceDefinition));
    assertSame(basicServiceDefinition,
        registry.addService(ServerServiceDefinition.builder(
            new ServiceDescriptor("basic")).build()));
  }
}
