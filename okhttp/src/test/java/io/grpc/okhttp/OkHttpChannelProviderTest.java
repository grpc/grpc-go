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

package io.grpc.okhttp;

import static org.junit.Assert.assertSame;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.ManagedChannelProvider;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link OkHttpChannelProvider}. */
@RunWith(JUnit4.class)
public class OkHttpChannelProviderTest {
  private OkHttpChannelProvider provider = new OkHttpChannelProvider();

  @Test
  public void provided() {
    for (ManagedChannelProvider current
        : ManagedChannelProvider.getCandidatesViaServiceLoader(getClass().getClassLoader())) {
      if (current instanceof OkHttpChannelProvider) {
        return;
      }
    }
    fail("ServiceLoader unable to load OkHttpChannelProvider");
  }

  @Test
  public void providedHardCoded() {
    for (ManagedChannelProvider current : ManagedChannelProvider.getCandidatesViaHardCoded()) {
      if (current instanceof OkHttpChannelProvider) {
        return;
      }
    }
    fail("Hard coded unable to load OkHttpChannelProvider");
  }

  @Test
  public void isAvailable() {
    assertTrue(provider.isAvailable());
  }

  @Test
  public void builderIsAOkHttpBuilder() {
    assertSame(OkHttpChannelBuilder.class, provider.builderForAddress("localhost", 443).getClass());
  }
}
