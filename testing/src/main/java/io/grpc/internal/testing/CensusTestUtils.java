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

package io.grpc.internal.testing;

import static com.google.common.base.Charsets.UTF_8;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.census.CensusContext;
import com.google.census.CensusContextFactory;
import com.google.census.Metric;
import com.google.census.MetricMap;
import com.google.census.MetricName;
import com.google.census.TagKey;
import com.google.census.TagValue;
import com.google.common.collect.ImmutableMap;

import java.nio.ByteBuffer;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

import javax.annotation.Nullable;

public class CensusTestUtils {
  private CensusTestUtils() {
  }

  public static class MetricsRecord {
    public final ImmutableMap<TagKey, TagValue> tags;
    public final MetricMap metrics;

    private MetricsRecord(ImmutableMap<TagKey, TagValue> tags, MetricMap metrics) {
      this.tags = tags;
      this.metrics = metrics;
    }

    /**
     * Returns the value of a metric, or {@code null} if not found.
     */
    @Nullable
    public Double getMetric(MetricName metricName) {
      for (Metric m : metrics) {
        if (m.getName().equals(metricName)) {
          return m.getValue();
        }
      }
      return null;
    }

    /**
     * Returns the value of a metric converted to long, or throw if not found.
     */
    public long getMetricAsLongOrFail(MetricName metricName) {
      Double doubleValue = getMetric(metricName);
      checkNotNull(doubleValue, "Metric not found: %s", metricName.toString());
      long longValue = (long) (Math.abs(doubleValue) + 0.0001);
      if (doubleValue < 0) {
        longValue = -longValue;
      }
      return longValue;
    }
  }

  public static final TagKey EXTRA_TAG = new TagKey("/rpc/test/extratag");

  private static final String EXTRA_TAG_HEADER_VALUE_PREFIX = "extratag:";
  private static final String NO_EXTRA_TAG_HEADER_VALUE_PREFIX = "noextratag";

  /**
   * A factory that makes fake {@link CensusContext}s and saves the created contexts to be
   * accessible from {@link #pollContextOrFail}.  The contexts it has created would save metrics
   * records to be accessible from {@link #pollRecord()} and {@link #pollRecord(long, TimeUnit)},
   * until {@link #rolloverRecords} is called.
   */
  public static final class FakeCensusContextFactory extends CensusContextFactory {
    private BlockingQueue<MetricsRecord> records;
    public final BlockingQueue<FakeCensusContext> contexts =
        new LinkedBlockingQueue<FakeCensusContext>();
    private final FakeCensusContext defaultContext;

    /**
     * Constructor.
     */
    public FakeCensusContextFactory() {
      rolloverRecords();
      defaultContext = new FakeCensusContext(ImmutableMap.<TagKey, TagValue>of(), this);
      // The records on the default context is not visible from pollRecord(), just like it's
      // not visible from pollContextOrFail() either.
      rolloverRecords();
    }

    public CensusContext pollContextOrFail() {
      CensusContext cc = contexts.poll();
      return checkNotNull(cc);
    }

    public MetricsRecord pollRecord() {
      return getCurrentRecordSink().poll();
    }

    public MetricsRecord pollRecord(long timeout, TimeUnit unit) throws InterruptedException {
      return getCurrentRecordSink().poll(timeout, unit);
    }

    @Override
    public CensusContext deserialize(ByteBuffer buffer) {
      String serializedString = new String(buffer.array(), UTF_8);
      if (serializedString.startsWith(EXTRA_TAG_HEADER_VALUE_PREFIX)) {
        return getDefault().with(EXTRA_TAG,
            new TagValue(serializedString.substring(EXTRA_TAG_HEADER_VALUE_PREFIX.length())));
      } else if (serializedString.startsWith(NO_EXTRA_TAG_HEADER_VALUE_PREFIX)) {
        return getDefault();
      } else {
        return null;
      }
    }

    @Override
    public FakeCensusContext getDefault() {
      return defaultContext;
    }

    /**
     * Disconnect this factory with the contexts it has created so far.  The records from those
     * contexts will not show up in {@link #pollRecord}.  Useful for isolating the records between
     * test cases.
     */
    // This needs to be synchronized with getCurrentRecordSink() which may run concurrently.
    public synchronized void rolloverRecords() {
      records = new LinkedBlockingQueue<MetricsRecord>();
    }

    private synchronized BlockingQueue<MetricsRecord> getCurrentRecordSink() {
      return records;
    }
  }

  public static class FakeCensusContext extends CensusContext {
    private final ImmutableMap<TagKey, TagValue> tags;
    private final FakeCensusContextFactory factory;
    private final BlockingQueue<MetricsRecord> recordSink;

    private FakeCensusContext(ImmutableMap<TagKey, TagValue> tags,
        FakeCensusContextFactory factory) {
      this.tags = tags;
      this.factory = factory;
      this.recordSink = factory.getCurrentRecordSink();
    }

    @Override
    public Builder builder() {
      return new FakeCensusContextBuilder(this);
    }

    @Override
    public CensusContext record(MetricMap metrics) {
      recordSink.add(new MetricsRecord(tags, metrics));
      return this;
    }

    @Override
    public ByteBuffer serialize() {
      TagValue extraTagValue = tags.get(EXTRA_TAG);
      if (extraTagValue == null) {
        return ByteBuffer.wrap(NO_EXTRA_TAG_HEADER_VALUE_PREFIX.getBytes(UTF_8));
      } else {
        return ByteBuffer.wrap(
            (EXTRA_TAG_HEADER_VALUE_PREFIX + extraTagValue.toString()).getBytes(UTF_8));
      }
    }

    @Override
    public String toString() {
      return "[tags=" + tags + "]";
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof FakeCensusContext)) {
        return false;
      }
      FakeCensusContext otherCtx = (FakeCensusContext) other;
      return tags.equals(otherCtx.tags);
    }

    @Override
    public int hashCode() {
      return tags.hashCode();
    }
  }

  private static class FakeCensusContextBuilder extends CensusContext.Builder {
    private final ImmutableMap.Builder<TagKey, TagValue> tagsBuilder = ImmutableMap.builder();
    private final FakeCensusContext base;

    private FakeCensusContextBuilder(FakeCensusContext base) {
      this.base = base;
      tagsBuilder.putAll(base.tags);
    }

    @Override
    public CensusContext.Builder set(TagKey key, TagValue value) {
      tagsBuilder.put(key, value);
      return this;
    }

    @Override
    public CensusContext build() {
      FakeCensusContext context = new FakeCensusContext(tagsBuilder.build(), base.factory);
      base.factory.contexts.add(context);
      return context;
    }
  }
}
