/*
 * Copyright 2016 The gRPC Authors
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

import com.google.common.base.Function;
import com.google.common.collect.ImmutableMap;
import com.google.common.collect.Iterators;
import com.google.common.collect.Maps;
import io.opencensus.common.Scope;
import io.opencensus.stats.Measure;
import io.opencensus.stats.MeasureMap;
import io.opencensus.stats.StatsRecorder;
import io.opencensus.tags.Tag;
import io.opencensus.tags.TagContext;
import io.opencensus.tags.TagContextBuilder;
import io.opencensus.tags.TagKey;
import io.opencensus.tags.TagValue;
import io.opencensus.tags.Tagger;
import io.opencensus.tags.propagation.TagContextBinarySerializer;
import io.opencensus.tags.propagation.TagContextDeserializationException;
import io.opencensus.tags.unsafe.ContextUtils;
import io.opencensus.trace.Annotation;
import io.opencensus.trace.AttributeValue;
import io.opencensus.trace.EndSpanOptions;
import io.opencensus.trace.Link;
import io.opencensus.trace.MessageEvent;
import io.opencensus.trace.Sampler;
import io.opencensus.trace.Span;
import io.opencensus.trace.SpanBuilder;
import io.opencensus.trace.SpanContext;
import io.opencensus.trace.SpanId;
import io.opencensus.trace.TraceId;
import io.opencensus.trace.TraceOptions;
import java.util.EnumSet;
import java.util.Iterator;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import javax.annotation.Nullable;

public class StatsTestUtils {
  private StatsTestUtils() {
  }

  public static class MetricsRecord {

    public final ImmutableMap<TagKey, TagValue> tags;
    public final ImmutableMap<Measure, Number> metrics;

    private MetricsRecord(
        ImmutableMap<TagKey, TagValue> tags, ImmutableMap<Measure, Number> metrics) {
      this.tags = tags;
      this.metrics = metrics;
    }

    /**
     * Returns the value of a metric, or {@code null} if not found.
     */
    @Nullable
    public Double getMetric(Measure measure) {
      for (Map.Entry<Measure, Number> m : metrics.entrySet()) {
        if (m.getKey().equals(measure)) {
          Number value = m.getValue();
          if (value instanceof Double) {
            return (Double) value;
          } else if (value instanceof Long) {
            return (double) (Long) value;
          }
          throw new AssertionError("Unexpected measure value type: " + value.getClass().getName());
        }
      }
      return null;
    }

    /**
     * Returns the value of a metric converted to long, or throw if not found.
     */
    public long getMetricAsLongOrFail(Measure measure) {
      Double doubleValue = getMetric(measure);
      checkNotNull(doubleValue, "Measure not found: %s", measure.getName());
      long longValue = (long) (Math.abs(doubleValue) + 0.0001);
      if (doubleValue < 0) {
        longValue = -longValue;
      }
      return longValue;
    }

    @Override
    public String toString() {
      return "[tags=" + tags + ", metrics=" + metrics + "]";
    }
  }

  /**
   * This tag will be propagated by {@link FakeTagger} on the wire.
   */
  public static final TagKey EXTRA_TAG = TagKey.create("/rpc/test/extratag");

  private static final String EXTRA_TAG_HEADER_VALUE_PREFIX = "extratag:";

  /**
   * A {@link Tagger} implementation that saves metrics records to be accessible from {@link
   * #pollRecord()} and {@link #pollRecord(long, TimeUnit)}, until {@link #rolloverRecords} is
   * called.
   */
  public static final class FakeStatsRecorder extends StatsRecorder {

    private BlockingQueue<MetricsRecord> records;

    public FakeStatsRecorder() {
      rolloverRecords();
    }

    @Override
    public MeasureMap newMeasureMap() {
      return new FakeStatsRecord(this);
    }

    public MetricsRecord pollRecord() {
      return getCurrentRecordSink().poll();
    }

    public MetricsRecord pollRecord(long timeout, TimeUnit unit) throws InterruptedException {
      return getCurrentRecordSink().poll(timeout, unit);
    }

    /**
     * Disconnect this tagger with the contexts it has created so far.  The records from those
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

  public static final class FakeTagger extends Tagger {

    @Override
    public FakeTagContext empty() {
      return FakeTagContext.EMPTY;
    }

    @Override
    public TagContext getCurrentTagContext() {
      return ContextUtils.TAG_CONTEXT_KEY.get();
    }

    @Override
    public TagContextBuilder emptyBuilder() {
      return new FakeTagContextBuilder(ImmutableMap.<TagKey, TagValue>of());
    }

    @Override
    public FakeTagContextBuilder toBuilder(TagContext tags) {
      return new FakeTagContextBuilder(getTags(tags));
    }

    @Override
    public TagContextBuilder currentBuilder() {
      throw new UnsupportedOperationException();
    }

    @Override
    public Scope withTagContext(TagContext tags) {
      throw new UnsupportedOperationException();
    }
  }

  public static final class FakeTagContextBinarySerializer extends TagContextBinarySerializer {

    private final FakeTagger tagger = new FakeTagger();

    @Override
    public TagContext fromByteArray(byte[] bytes) throws TagContextDeserializationException {
      String serializedString = new String(bytes, UTF_8);
      if (serializedString.startsWith(EXTRA_TAG_HEADER_VALUE_PREFIX)) {
        return tagger.emptyBuilder()
            .put(EXTRA_TAG,
                TagValue.create(serializedString.substring(EXTRA_TAG_HEADER_VALUE_PREFIX.length())))
            .build();
      } else {
        throw new TagContextDeserializationException("Malformed value");
      }
    }

    @Override
    public byte[] toByteArray(TagContext tags) {
      TagValue extraTagValue = getTags(tags).get(EXTRA_TAG);
      if (extraTagValue == null) {
        throw new UnsupportedOperationException("TagContext must contain EXTRA_TAG");
      }
      return (EXTRA_TAG_HEADER_VALUE_PREFIX + extraTagValue.asString()).getBytes(UTF_8);
    }
  }

  public static final class FakeStatsRecord extends MeasureMap {

    private final BlockingQueue<MetricsRecord> recordSink;
    public final Map<Measure, Number> metrics = Maps.newHashMap();

    private FakeStatsRecord(FakeStatsRecorder statsRecorder) {
      this.recordSink = statsRecorder.getCurrentRecordSink();
    }

    @Override
    public MeasureMap put(Measure.MeasureDouble measure, double value) {
      metrics.put(measure, value);
      return this;
    }

    @Override
    public MeasureMap put(Measure.MeasureLong measure, long value) {
      metrics.put(measure, value);
      return this;
    }

    @Override
    public void record(TagContext tags) {
      recordSink.add(new MetricsRecord(getTags(tags), ImmutableMap.copyOf(metrics)));
    }

    @Override
    public void record() {
      throw new UnsupportedOperationException();
    }
  }

  public static final class FakeTagContext extends TagContext {

    private static final FakeTagContext EMPTY =
        new FakeTagContext(ImmutableMap.<TagKey, TagValue>of());

    private final ImmutableMap<TagKey, TagValue> tags;

    private FakeTagContext(ImmutableMap<TagKey, TagValue> tags) {
      this.tags = tags;
    }

    public ImmutableMap<TagKey, TagValue> getTags() {
      return tags;
    }

    @Override
    public String toString() {
      return "[tags=" + tags + "]";
    }

    @Override
    protected Iterator<Tag> getIterator() {
      return Iterators.transform(
          tags.entrySet().iterator(),
          new Function<Map.Entry<TagKey, TagValue>, Tag>() {
            @Override
            public Tag apply(@Nullable Map.Entry<TagKey, TagValue> entry) {
              return Tag.create(entry.getKey(), entry.getValue());
            }
          });
    }
  }

  public static class FakeTagContextBuilder extends TagContextBuilder {

    private final Map<TagKey, TagValue> tagsBuilder = Maps.newHashMap();

    private FakeTagContextBuilder(Map<TagKey, TagValue> tags) {
      tagsBuilder.putAll(tags);
    }

    @Override
    public TagContextBuilder put(TagKey key, TagValue value) {
      tagsBuilder.put(key, value);
      return this;
    }

    @Override
    public TagContextBuilder remove(TagKey key) {
      tagsBuilder.remove(key);
      return this;
    }

    @Override
    public TagContext build() {
      FakeTagContext context = new FakeTagContext(ImmutableMap.copyOf(tagsBuilder));
      return context;
    }

    @Override
    public Scope buildScoped() {
      throw new UnsupportedOperationException();
    }
  }

  // This method handles the default TagContext, which isn't an instance of FakeTagContext.
  private static ImmutableMap<TagKey, TagValue> getTags(TagContext tags) {
    return tags instanceof FakeTagContext
        ? ((FakeTagContext) tags).getTags()
        : ImmutableMap.<TagKey, TagValue>of();
  }

  // TODO(bdrutu): Remove this class after OpenCensus releases support for this class.
  public static class MockableSpan extends Span {
    /**
     * Creates a MockableSpan with a random trace ID and span ID.
     */
    public static MockableSpan generateRandomSpan(Random random) {
      return new MockableSpan(
          SpanContext.create(
              TraceId.generateRandomId(random),
              SpanId.generateRandomId(random),
              TraceOptions.DEFAULT),
          null);
    }

    @Override
    public void putAttributes(Map<String, AttributeValue> attributes) {}

    @Override
    public void addAnnotation(String description, Map<String, AttributeValue> attributes) {}

    @Override
    public void addAnnotation(Annotation annotation) {}

    @Override
    public void addMessageEvent(MessageEvent messageEvent) {}

    @Override
    public void addLink(Link link) {}

    @Override
    public void end(EndSpanOptions options) {}

    private MockableSpan(SpanContext context, @Nullable EnumSet<Options> options) {
      super(context, options);
    }

    /**
     * Mockable implementation for the {@link SpanBuilder} class.
     *
     * <p>Not {@code final} to allow easy mocking.
     *
     */
    public static class Builder extends SpanBuilder {

      @Override
      public SpanBuilder setSampler(Sampler sampler) {
        return this;
      }

      @Override
      public SpanBuilder setParentLinks(List<Span> parentLinks) {
        return this;
      }

      @Override
      public SpanBuilder setRecordEvents(boolean recordEvents) {
        return this;
      }

      @Override
      public Span startSpan() {
        return null;
      }
    }
  }
}
