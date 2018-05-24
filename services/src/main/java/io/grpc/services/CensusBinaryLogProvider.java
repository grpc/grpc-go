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

import io.grpc.CallOptions;
import io.opencensus.trace.Span;
import io.opencensus.trace.Tracing;
import java.io.IOException;
import java.nio.ByteBuffer;

final class CensusBinaryLogProvider extends BinaryLogProviderImpl {

  public CensusBinaryLogProvider() throws IOException {
    super();
  }

  CensusBinaryLogProvider(BinaryLogSink sink, String configStr) throws IOException {
    super(sink, configStr);
  }

  @Override
  protected CallId getServerCallId() {
    Span currentSpan = Tracing.getTracer().getCurrentSpan();
    return new CallId(
        0,
        ByteBuffer.wrap(
            currentSpan.getContext().getSpanId().getBytes()).getLong());
  }

  @Override
  protected CallId getClientCallId(CallOptions options) {
    return options.getOption(BinaryLogProvider.CLIENT_CALL_ID_CALLOPTION_KEY);
  }
}
