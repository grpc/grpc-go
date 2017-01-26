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

import com.google.instrumentation.stats.MeasurementMap;
import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.StatsContextFactory;
import com.google.instrumentation.stats.TagKey;
import com.google.instrumentation.stats.TagValue;
import java.io.InputStream;
import java.io.OutputStream;

public final class NoopStatsContextFactory extends StatsContextFactory {
  private static final StatsContext DEFAULT_CONTEXT = new NoopStatsContext();
  private static final StatsContext.Builder BUILDER = new NoopContextBuilder();

  public static final StatsContextFactory INSTANCE = new NoopStatsContextFactory();

  private NoopStatsContextFactory() {
  }

  @Override
  public StatsContext deserialize(InputStream is) {
    return DEFAULT_CONTEXT;
  }

  @Override
  public StatsContext getDefault() {
    return DEFAULT_CONTEXT;
  }

  private static class NoopStatsContext extends StatsContext {
    @Override
    public Builder builder() {
      return BUILDER;
    }

    @Override
    public StatsContext record(MeasurementMap metrics) {
      return DEFAULT_CONTEXT;
    }

    @Override
    public void serialize(OutputStream os) {
      return;
    }
  }

  private static class NoopContextBuilder extends StatsContext.Builder {
    @Override
    public StatsContext.Builder set(TagKey key, TagValue value) {
      return this;
    }

    @Override
    public StatsContext build() {
      return DEFAULT_CONTEXT;
    }
  }
}
