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

package io.grpc;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import io.grpc.HandlerRegistry.Method;

import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mockito;

/** Unit tests for {@link MutableHandlerRegistryImpl}. */
@RunWith(JUnit4.class)
public class MutableHandlerRegistryImplTest {
  private MutableHandlerRegistry registry = new MutableHandlerRegistryImpl();
  @SuppressWarnings("unchecked")
  private Marshaller<String> requestMarshaller = mock(Marshaller.class);
  @SuppressWarnings("unchecked")
  private Marshaller<Integer> responseMarshaller = mock(Marshaller.class);
  @SuppressWarnings("unchecked")
  private ServerCallHandler<String, Integer> handler = mock(ServerCallHandler.class);
  private ServerServiceDefinition basicServiceDefinition = ServerServiceDefinition.builder("basic")
      .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();
  @SuppressWarnings("rawtypes")
  private ServerMethodDefinition flowMethodDefinition = basicServiceDefinition.getMethods().get(0);
  private ServerServiceDefinition multiServiceDefinition = ServerServiceDefinition.builder("multi")
      .addMethod("couple", requestMarshaller, responseMarshaller, handler)
      .addMethod("few", requestMarshaller, responseMarshaller, handler).build();
  @SuppressWarnings("rawtypes")
  private ServerMethodDefinition coupleMethodDefinition =
      multiServiceDefinition.getMethod("couple");
  @SuppressWarnings("rawtypes")
  private ServerMethodDefinition fewMethodDefinition = multiServiceDefinition.getMethod("few");

  @After
  public void makeSureMocksUnused() {
    Mockito.verifyZeroInteractions(requestMarshaller);
    Mockito.verifyZeroInteractions(responseMarshaller);
    Mockito.verifyZeroInteractions(handler);
  }

  @Test
  public void simpleLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    Method method = registry.lookupMethod("/basic/flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/basic/flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/basic/flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());

    assertNull(registry.lookupMethod("basic/flow"));
    assertNull(registry.lookupMethod("/basic/basic"));
    assertNull(registry.lookupMethod("/flow/flow"));
    assertNull(registry.lookupMethod("/completely/random"));
  }

  @Test
  public void multiServiceLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNull(registry.addService(multiServiceDefinition));

    Method method = registry.lookupMethod("/basic/flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/multi/couple");
    assertSame(coupleMethodDefinition, method.getMethodDefinition());
    assertSame(multiServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/multi/few");
    assertSame(fewMethodDefinition, method.getMethodDefinition());
    assertSame(multiServiceDefinition, method.getServiceDefinition());
  }

  @Test
  public void removeAndLookup() {
    assertNull(registry.addService(multiServiceDefinition));
    assertNotNull(registry.lookupMethod("/multi/couple"));
    assertNotNull(registry.lookupMethod("/multi/few"));
    assertTrue(registry.removeService(multiServiceDefinition));
    assertNull(registry.lookupMethod("/multi/couple"));
    assertNull(registry.lookupMethod("/multi/few"));
  }

  @Test
  public void replaceAndLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNotNull(registry.lookupMethod("/basic/flow"));
    ServerServiceDefinition replaceServiceDefinition = ServerServiceDefinition.builder("basic")
        .addMethod("another", requestMarshaller, responseMarshaller, handler).build();
    ServerMethodDefinition<?, ?> anotherMethodDefinition =
        replaceServiceDefinition.getMethods().get(0);
    assertSame(basicServiceDefinition, registry.addService(replaceServiceDefinition));

    assertNull(registry.lookupMethod("/basic/flow"));
    Method method = registry.lookupMethod("/basic/another");
    assertSame(anotherMethodDefinition, method.getMethodDefinition());
    assertSame(replaceServiceDefinition, method.getServiceDefinition());
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
    assertFalse(registry.removeService(ServerServiceDefinition.builder("basic").build()));
  }

  @Test
  public void initialAddReturnsNull() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNull(registry.addService(multiServiceDefinition));
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
        registry.addService(ServerServiceDefinition.builder("basic").build()));
  }
}
