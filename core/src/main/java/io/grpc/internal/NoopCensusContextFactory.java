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

import com.google.census.CensusContext;
import com.google.census.CensusContextFactory;
import com.google.census.MetricMap;
import com.google.census.TagKey;
import com.google.census.TagValue;

import java.nio.ByteBuffer;

public final class NoopCensusContextFactory extends CensusContextFactory {
  private static final byte[] SERIALIZED_BYTES = new byte[0];
  private static final CensusContext DEFAULT_CONTEXT = new NoopCensusContext();
  private static final CensusContext.Builder BUILDER = new NoopContextBuilder();

  public static final CensusContextFactory INSTANCE = new NoopCensusContextFactory();

  private NoopCensusContextFactory() {
  }

  @Override
  public CensusContext deserialize(ByteBuffer buffer) {
    return DEFAULT_CONTEXT;
  }

  @Override
  public CensusContext getDefault() {
    return DEFAULT_CONTEXT;
  }

  private static class NoopCensusContext extends CensusContext {
    @Override
    public Builder builder() {
      return BUILDER;
    }

    @Override
    public CensusContext record(MetricMap metrics) {
      return DEFAULT_CONTEXT;
    }

    @Override
    public ByteBuffer serialize() {
      return ByteBuffer.wrap(SERIALIZED_BYTES).asReadOnlyBuffer();
    }
  }

  private static class NoopContextBuilder extends CensusContext.Builder {
    @Override
    public CensusContext.Builder set(TagKey key, TagValue value) {
      return this;
    }

    @Override
    public CensusContext build() {
      return DEFAULT_CONTEXT;
    }
  }
}
