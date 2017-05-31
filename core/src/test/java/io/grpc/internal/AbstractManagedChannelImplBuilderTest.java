/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.NameResolver;
import java.net.InetSocketAddress;
import java.net.URI;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AbstractManagedChannelImplBuilder}. */
@RunWith(JUnit4.class)
public class AbstractManagedChannelImplBuilderTest {
  @Test
  public void makeTargetStringForDirectAddress_scopedIpv6() throws Exception {
    InetSocketAddress address = new InetSocketAddress("0:0:0:0:0:0:0:0%0", 10005);
    assertEquals("/0:0:0:0:0:0:0:0%0:10005", address.toString());
    String target = AbstractManagedChannelImplBuilder.makeTargetStringForDirectAddress(address);
    URI uri = new URI(target);
    assertEquals("directaddress:////0:0:0:0:0:0:0:0%250:10005", target);
    assertEquals(target, uri.toString());
  }

  @Test
  public void idleTimeout() {
    class Builder extends AbstractManagedChannelImplBuilder<Builder> {
      Builder() {
        super("target");
      }

      @Override
      protected ClientTransportFactory buildTransportFactory() {
        throw new UnsupportedOperationException();
      }

      @Override
      public Builder usePlaintext(boolean value) {
        return this;
      }
    }

    Builder builder = new Builder();

    assertEquals(AbstractManagedChannelImplBuilder.IDLE_MODE_DEFAULT_TIMEOUT_MILLIS,
        builder.getIdleTimeoutMillis());

    builder.idleTimeout(Long.MAX_VALUE, TimeUnit.DAYS);
    assertEquals(ManagedChannelImpl.IDLE_TIMEOUT_MILLIS_DISABLE, builder.getIdleTimeoutMillis());

    builder.idleTimeout(AbstractManagedChannelImplBuilder.IDLE_MODE_MAX_TIMEOUT_DAYS,
        TimeUnit.DAYS);
    assertEquals(ManagedChannelImpl.IDLE_TIMEOUT_MILLIS_DISABLE, builder.getIdleTimeoutMillis());

    try {
      builder.idleTimeout(0, TimeUnit.SECONDS);
      fail("Should throw");
    } catch (IllegalArgumentException e) {
      // expected
    }

    builder.idleTimeout(1, TimeUnit.NANOSECONDS);
    assertEquals(AbstractManagedChannelImplBuilder.IDLE_MODE_MIN_TIMEOUT_MILLIS,
        builder.getIdleTimeoutMillis());

    builder.idleTimeout(30, TimeUnit.SECONDS);
    assertEquals(TimeUnit.SECONDS.toMillis(30), builder.getIdleTimeoutMillis());
  }

  @Test
  public void overrideAuthorityNameResolverWrapsDelegateTest() {
    NameResolver nameResolverMock = mock(NameResolver.class);
    NameResolver.Factory wrappedFactory = mock(NameResolver.Factory.class);
    when(wrappedFactory.newNameResolver(any(URI.class), any(Attributes.class)))
      .thenReturn(nameResolverMock);
    String override = "override:5678";
    NameResolver.Factory factory =
        new AbstractManagedChannelImplBuilder.OverrideAuthorityNameResolverFactory(wrappedFactory,
          override);
    NameResolver nameResolver = factory.newNameResolver(URI.create("dns:///localhost:443"),
        Attributes.EMPTY);
    assertNotNull(nameResolver);
    assertEquals(override, nameResolver.getServiceAuthority());
  }

  @Test
  public void overrideAuthorityNameResolverWontWrapNullTest() {
    NameResolver.Factory wrappedFactory = mock(NameResolver.Factory.class);
    when(wrappedFactory.newNameResolver(any(URI.class), any(Attributes.class))).thenReturn(null);
    NameResolver.Factory factory =
        new AbstractManagedChannelImplBuilder.OverrideAuthorityNameResolverFactory(wrappedFactory,
            "override:5678");
    assertEquals(null,
        factory.newNameResolver(URI.create("dns:///localhost:443"), Attributes.EMPTY));
  }
}
