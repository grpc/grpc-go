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

package io.grpc.internal.testing;

import static com.google.common.base.Charsets.UTF_8;
import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.collect.ImmutableMap;
import com.google.instrumentation.stats.MeasurementDescriptor;
import com.google.instrumentation.stats.MeasurementMap;
import com.google.instrumentation.stats.MeasurementValue;
import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.StatsContextFactory;
import com.google.instrumentation.stats.TagKey;
import com.google.instrumentation.stats.TagValue;
import io.grpc.internal.IoUtils;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

public class StatsTestUtils {
  private StatsTestUtils() {
  }

  public static class MetricsRecord {
    public final ImmutableMap<TagKey, TagValue> tags;
    public final MeasurementMap metrics;

    private MetricsRecord(ImmutableMap<TagKey, TagValue> tags, MeasurementMap metrics) {
      this.tags = tags;
      this.metrics = metrics;
    }

    /**
     * Returns the value of a metric, or {@code null} if not found.
     */
    @Nullable
    public Double getMetric(MeasurementDescriptor metricName) {
      for (MeasurementValue m : metrics) {
        if (m.getMeasurement().equals(metricName)) {
          return m.getValue();
        }
      }
      return null;
    }

    /**
     * Returns the value of a metric converted to long, or throw if not found.
     */
    public long getMetricAsLongOrFail(MeasurementDescriptor metricName) {
      Double doubleValue = getMetric(metricName);
      checkNotNull(doubleValue, "Metric not found: %s", metricName.toString());
      long longValue = (long) (Math.abs(doubleValue) + 0.0001);
      if (doubleValue < 0) {
        longValue = -longValue;
      }
      return longValue;
    }
  }

  /**
   * This tag will be propagated by {@link FakeStatsContextFactory} on the wire.
   */
  public static final TagKey EXTRA_TAG = TagKey.create("/rpc/test/extratag");

  private static final String EXTRA_TAG_HEADER_VALUE_PREFIX = "extratag:";
  private static final String NO_EXTRA_TAG_HEADER_VALUE_PREFIX = "noextratag";

  /**
   * A factory that makes fake {@link StatsContext}s and saves the created contexts to be
   * accessible from {@link #pollContextOrFail}.  The contexts it has created would save metrics
   * records to be accessible from {@link #pollRecord()} and {@link #pollRecord(long, TimeUnit)},
   * until {@link #rolloverRecords} is called.
   */
  public static final class FakeStatsContextFactory extends StatsContextFactory {
    private BlockingQueue<MetricsRecord> records;
    public final BlockingQueue<FakeStatsContext> contexts =
        new LinkedBlockingQueue<FakeStatsContext>();
    private final FakeStatsContext defaultContext;

    /**
     * Constructor.
     */
    public FakeStatsContextFactory() {
      rolloverRecords();
      defaultContext = new FakeStatsContext(ImmutableMap.<TagKey, TagValue>of(), this);
      // The records on the default context is not visible from pollRecord(), just like it's
      // not visible from pollContextOrFail() either.
      rolloverRecords();
    }

    public StatsContext pollContextOrFail() {
      StatsContext cc = contexts.poll();
      return checkNotNull(cc);
    }

    public MetricsRecord pollRecord() {
      return getCurrentRecordSink().poll();
    }

    public MetricsRecord pollRecord(long timeout, TimeUnit unit) throws InterruptedException {
      return getCurrentRecordSink().poll(timeout, unit);
    }

    @Override
    public StatsContext deserialize(InputStream buffer) throws IOException {
      String serializedString;
      try {
        serializedString = new String(IoUtils.toByteArray(buffer), UTF_8);
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
      if (serializedString.startsWith(EXTRA_TAG_HEADER_VALUE_PREFIX)) {
        return getDefault().with(EXTRA_TAG,
            TagValue.create(serializedString.substring(EXTRA_TAG_HEADER_VALUE_PREFIX.length())));
      } else if (serializedString.startsWith(NO_EXTRA_TAG_HEADER_VALUE_PREFIX)) {
        return getDefault();
      } else {
        throw new IOException("Malformed value");
      }
    }

    @Override
    public FakeStatsContext getDefault() {
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

  public static class FakeStatsContext extends StatsContext {
    private final ImmutableMap<TagKey, TagValue> tags;
    private final FakeStatsContextFactory factory;
    private final BlockingQueue<MetricsRecord> recordSink;

    private FakeStatsContext(ImmutableMap<TagKey, TagValue> tags,
        FakeStatsContextFactory factory) {
      this.tags = tags;
      this.factory = factory;
      this.recordSink = factory.getCurrentRecordSink();
    }

    @Override
    public Builder builder() {
      return new FakeStatsContextBuilder(this);
    }

    @Override
    public StatsContext record(MeasurementMap metrics) {
      recordSink.add(new MetricsRecord(tags, metrics));
      return this;
    }

    @Override
    public void serialize(OutputStream os) {
      TagValue extraTagValue = tags.get(EXTRA_TAG);
      try {
        if (extraTagValue == null) {
          os.write(NO_EXTRA_TAG_HEADER_VALUE_PREFIX.getBytes(UTF_8));
        } else {
          os.write((EXTRA_TAG_HEADER_VALUE_PREFIX + extraTagValue.toString()).getBytes(UTF_8));
        }
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
    }

    @Override
    public String toString() {
      return "[tags=" + tags + "]";
    }

    @Override
    public boolean equals(Object other) {
      if (!(other instanceof FakeStatsContext)) {
        return false;
      }
      FakeStatsContext otherCtx = (FakeStatsContext) other;
      return tags.equals(otherCtx.tags);
    }

    @Override
    public int hashCode() {
      return tags.hashCode();
    }
  }

  private static class FakeStatsContextBuilder extends StatsContext.Builder {
    private final ImmutableMap.Builder<TagKey, TagValue> tagsBuilder = ImmutableMap.builder();
    private final FakeStatsContext base;

    private FakeStatsContextBuilder(FakeStatsContext base) {
      this.base = base;
      tagsBuilder.putAll(base.tags);
    }

    @Override
    public StatsContext.Builder set(TagKey key, TagValue value) {
      tagsBuilder.put(key, value);
      return this;
    }

    @Override
    public StatsContext build() {
      FakeStatsContext context = new FakeStatsContext(tagsBuilder.build(), base.factory);
      base.factory.contexts.add(context);
      return context;
    }
  }
}
