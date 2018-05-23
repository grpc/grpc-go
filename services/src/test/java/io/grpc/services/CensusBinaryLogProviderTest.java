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

import static com.google.common.truth.Truth.assertThat;
import static io.opencensus.trace.unsafe.ContextUtils.CONTEXT_SPAN_KEY;

import io.grpc.BinaryLog.CallId;
import io.grpc.BinaryLogProvider;
import io.grpc.CallOptions;
import io.grpc.Context;
import io.grpc.internal.testing.StatsTestUtils.MockableSpan;
import java.nio.ByteBuffer;
import java.util.Random;
import java.util.concurrent.Callable;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

/**
 * Tests for {@link CensusBinaryLogProvider}.
 */
@RunWith(JUnit4.class)
public class CensusBinaryLogProviderTest {
  @Mock
  private BinaryLogSink sink;

  public CensusBinaryLogProviderTest() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void serverCallIdFromCensus() throws Exception {
    final MockableSpan mockableSpan = MockableSpan.generateRandomSpan(new Random(0));
    Context context = Context.current().withValue(CONTEXT_SPAN_KEY, mockableSpan);
    context.call(new Callable<Void>() {
      @Override
      public Void call() throws Exception {
        CallId callId = new CensusBinaryLogProvider(sink, "*").getServerCallId();
        assertThat(callId.hi).isEqualTo(0);
        assertThat(ByteBuffer.wrap(mockableSpan.getContext().getSpanId().getBytes()).getLong())
            .isEqualTo(callId.lo);
        return null;
      }
    });
  }

  @Test
  public void clientCallId() throws Exception {
    CallId expected = new CallId(1234, 5677);
    CallId actual = new CensusBinaryLogProvider(sink, "*")
        .getClientCallId(
            CallOptions.DEFAULT.withOption(
                BinaryLogProvider.CLIENT_CALL_ID_CALLOPTION_KEY,
                expected));
    assertThat(actual).isEqualTo(expected);
  }
}
