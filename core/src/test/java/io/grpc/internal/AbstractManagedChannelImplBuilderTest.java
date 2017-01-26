/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;

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
}
