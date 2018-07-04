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

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;

import io.grpc.internal.DnsNameResolver.AddressResolver;
import io.grpc.internal.JndiResourceResolverFactory.JndiResourceResolver;
import io.grpc.internal.JndiResourceResolverFactory.JndiResourceResolver.SrvRecord;
import java.net.InetAddress;
import java.util.List;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link JndiResourceResolverFactory}.
 */
@RunWith(JUnit4.class)
public class JndiResourceResolverTest {

  @Test
  public void normalizeDataRemovesJndiFormattingForTxtRecords() {
    assertEquals("blah", JndiResourceResolver.unquote("blah"));
    assertEquals("", JndiResourceResolver.unquote("\"\""));
    assertEquals("blahblah", JndiResourceResolver.unquote("blah blah"));
    assertEquals("blahfoo blah", JndiResourceResolver.unquote("blah \"foo blah\""));
    assertEquals("blah blah", JndiResourceResolver.unquote("\"blah blah\""));
    assertEquals("blah\"blah", JndiResourceResolver.unquote("\"blah\\\"blah\""));
    assertEquals("blah\\blah", JndiResourceResolver.unquote("\"blah\\\\blah\""));
  }

  @Test
  public void jndiResolverWorks() throws Exception {
    Assume.assumeNoException(new JndiResourceResolverFactory().unavailabilityCause());

    AddressResolver addressResolver = new AddressResolver() {
      @Override
      public List<InetAddress> resolveAddress(String host) throws Exception {
        return null;
      }
    };
    JndiResourceResolver resolver = new JndiResourceResolver();
    try {
      resolver.resolveSrv(addressResolver, "localhost");
    } catch (javax.naming.CommunicationException e) {
      Assume.assumeNoException(e);
    } catch (javax.naming.NameNotFoundException e) {
      Assume.assumeNoException(e);
    }
  }

  @Test
  public void parseSrvRecord() {
    SrvRecord record = JndiResourceResolver.parseSrvRecord("0 0 1234 foo.bar.com");
    assertThat(record.host).isEqualTo("foo.bar.com");
    assertThat(record.port).isEqualTo(1234);
  }
}
