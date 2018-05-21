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

package io.grpc.services;

import static org.junit.Assert.assertNull;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;

import io.grpc.CallOptions;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Tests for {@link BinaryLogProviderImpl}. */
@RunWith(JUnit4.class)
public class BinaryLogProviderImplTest {
  @Test
  public void configStrNullTest() throws Exception {
    BinaryLogSink sink = mock(BinaryLogSink.class);
    BinaryLogProviderImpl binlog = new BinaryLogProviderImpl(sink, /*configStr=*/ null);
    assertNull(binlog.getServerInterceptor("package.service/method"));
    assertNull(binlog.getClientInterceptor("package.service/method", CallOptions.DEFAULT));
  }

  @Test
  public void configStrEmptyTest() throws Exception {
    BinaryLogSink sink = mock(BinaryLogSink.class);
    BinaryLogProviderImpl binlog = new BinaryLogProviderImpl(sink, "");
    assertNull(binlog.getServerInterceptor("package.service/method"));
    assertNull(binlog.getClientInterceptor("package.service/method", CallOptions.DEFAULT));
  }

  @Test
  public void closeTest() throws Exception {
    BinaryLogSink sink = mock(BinaryLogSink.class);
    BinaryLogProviderImpl log = new BinaryLogProviderImpl(sink, "*");
    verify(sink, never()).close();
    log.close();
    verify(sink).close();
  }
}
