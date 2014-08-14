package com.google.net.stubby;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import com.google.net.stubby.Server.CallHandler;
import com.google.net.stubby.Server.MethodDefinition;
import com.google.net.stubby.Server.ServiceDefinition;
import com.google.net.stubby.HandlerRegistry.Method;

import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mockito;

/** Unit tests for {@link MutableHandlerRegistryImpl}. */
@RunWith(JUnit4.class)
public class MutableHandlerRegistryImplTest {
  private MutableHandlerRegistry registry = new MutableHandlerRegistryImpl();
  private Marshaller<String> requestMarshaller = mock(Marshaller.class);
  private Marshaller<Integer> responseMarshaller = mock(Marshaller.class);
  private CallHandler<String, Integer> handler = mock(CallHandler.class);
  private ServiceDefinition basicServiceDefinition = ServiceDefinition.builder("basic")
        .addMethod("flow", requestMarshaller, responseMarshaller, handler).build();
  private MethodDefinition flowMethodDefinition = basicServiceDefinition.getMethods().get(0);
  private ServiceDefinition multiServiceDefinition = ServiceDefinition.builder("multi")
        .addMethod("couple", requestMarshaller, responseMarshaller, handler)
        .addMethod("few", requestMarshaller, responseMarshaller, handler).build();
  private MethodDefinition coupleMethodDefinition = multiServiceDefinition.getMethod("couple");
  private MethodDefinition fewMethodDefinition = multiServiceDefinition.getMethod("few");

  @After
  public void makeSureMocksUnused() {
    Mockito.verifyZeroInteractions(requestMarshaller);
    Mockito.verifyZeroInteractions(responseMarshaller);
    Mockito.verifyZeroInteractions(handler);
  }

  @Test
  public void simpleLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    Method method = registry.lookupMethod("/basic.flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/basic.flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/basic/flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());

    assertNull(registry.lookupMethod("basic.flow"));
    assertNull(registry.lookupMethod("/basic.basic"));
    assertNull(registry.lookupMethod("/flow.flow"));
    assertNull(registry.lookupMethod("/completely.random"));
  }

  @Test
  public void multiServiceLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNull(registry.addService(multiServiceDefinition));

    Method method = registry.lookupMethod("/basic.flow");
    assertSame(flowMethodDefinition, method.getMethodDefinition());
    assertSame(basicServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/multi.couple");
    assertSame(coupleMethodDefinition, method.getMethodDefinition());
    assertSame(multiServiceDefinition, method.getServiceDefinition());
    method = registry.lookupMethod("/multi.few");
    assertSame(fewMethodDefinition, method.getMethodDefinition());
    assertSame(multiServiceDefinition, method.getServiceDefinition());
  }

  @Test
  public void removeAndLookup() {
    assertNull(registry.addService(multiServiceDefinition));
    assertNotNull(registry.lookupMethod("/multi.couple"));
    assertNotNull(registry.lookupMethod("/multi.few"));
    assertTrue(registry.removeService(multiServiceDefinition));
    assertNull(registry.lookupMethod("/multi.couple"));
    assertNull(registry.lookupMethod("/multi.few"));
  }

  @Test
  public void replaceAndLookup() {
    assertNull(registry.addService(basicServiceDefinition));
    assertNotNull(registry.lookupMethod("/basic.flow"));
    ServiceDefinition replaceServiceDefinition = ServiceDefinition.builder("basic")
        .addMethod("another", requestMarshaller, responseMarshaller, handler).build();
    MethodDefinition anotherMethodDefinition = replaceServiceDefinition.getMethods().get(0);
    assertSame(basicServiceDefinition, registry.addService(replaceServiceDefinition));

    assertNull(registry.lookupMethod("/basic.flow"));
    Method method = registry.lookupMethod("/basic.another");
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
    assertFalse(registry.removeService(ServiceDefinition.builder("basic").build()));
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
        registry.addService(ServiceDefinition.builder("basic").build()));
  }
}
