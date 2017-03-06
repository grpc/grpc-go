/*
 * Copyright 2017, Google Inc. All rights reserved.
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

package io.grpc.services;

import static org.junit.Assert.assertEquals;

import com.google.instrumentation.common.Duration;
import com.google.instrumentation.common.Timestamp;
import com.google.instrumentation.stats.DistributionAggregation;
import com.google.instrumentation.stats.DistributionAggregation.Range;
import com.google.instrumentation.stats.DistributionAggregationDescriptor;
import com.google.instrumentation.stats.IntervalAggregation;
import com.google.instrumentation.stats.IntervalAggregation.Interval;
import com.google.instrumentation.stats.IntervalAggregationDescriptor;
import com.google.instrumentation.stats.MeasurementDescriptor;
import com.google.instrumentation.stats.MeasurementDescriptor.BasicUnit;
import com.google.instrumentation.stats.MeasurementDescriptor.MeasurementUnit;
import com.google.instrumentation.stats.Tag;
import com.google.instrumentation.stats.TagKey;
import com.google.instrumentation.stats.TagValue;
import com.google.instrumentation.stats.View.DistributionView;
import com.google.instrumentation.stats.View.IntervalView;
import com.google.instrumentation.stats.ViewDescriptor.DistributionViewDescriptor;
import com.google.instrumentation.stats.ViewDescriptor.IntervalViewDescriptor;
import com.google.instrumentation.stats.proto.CensusProto;
import io.grpc.instrumentation.v1alpha.StatsResponse;
import java.util.Arrays;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link MonitoringUtil}. */
@RunWith(JUnit4.class)
public class MonitoringUtilTest {
  @Test
  public void buildCanonicalRpcStatsViewForDistributionView() throws Exception {
    assertEquals(
        DISTRIBUTION_STATS_RESPONSE_PROTO,
        MonitoringUtil.buildCanonicalRpcStatsView(DISTRIBUTION_VIEW));
  }

  @Test
  public void buildCanonicalRpcStatsViewForIntervalView() throws Exception {
    assertEquals(
        INTERVAL_STATS_RESPONSE_PROTO, MonitoringUtil.buildCanonicalRpcStatsView(INTERVAL_VIEW));
  }

  @Test
  public void serializeMeasurementDescriptor() throws Exception {
    assertEquals(
        MEASUREMENT_DESC_PROTO,
        MonitoringUtil.serializeMeasurementDescriptor(measurementDescriptor));
  }

  @Test
  public void serializeMeasurementUnit() throws Exception {
    assertEquals(MEASUREMENT_UNIT_PROTO, MonitoringUtil.serializeMeasurementUnit(MEASUREMENT_UNIT));
  }

  @Test
  public void serializeViewDescriptorForDistributionView() throws Exception {
    assertEquals(
        DISTRIBUTION_VIEW_DESC_PROTO,
        MonitoringUtil.serializeViewDescriptor(DISTRIBUTION_VIEW_DESC));
  }

  @Test
  public void serializeViewDescriptorForIntervalView() throws Exception {
    assertEquals(
        INTERVAL_VIEW_DESC_PROTO, MonitoringUtil.serializeViewDescriptor(INTERVAL_VIEW_DESC));
  }

  @Test
  public void serializeDistributionAggregationDescriptor() throws Exception {
    assertEquals(
        DISTRIBUTION_AGG_DESC_PROTO,
        MonitoringUtil.serializeDistributionAggregationDescriptor(DISTRIBUTION_AGG_DESC));
  }

  @Test
  public void serializeIntervalAggregationDescriptor() throws Exception {
    assertEquals(
        INTERVAL_AGG_DESC_PROTO,
        MonitoringUtil.serializeIntervalAggregationDescriptor(INTERVAL_AGG_DESC));
  }

  @Test
  public void serializeDuration() throws Exception {
    assertEquals(DURATION_PROTO, MonitoringUtil.serializeDuration(DURATION));
  }

  @Test
  public void serializeViewWithDistributionView() throws Exception {
    assertEquals(
        VIEW_WITH_DISTRIBUTION_VIEW_PROTO, MonitoringUtil.serializeView(DISTRIBUTION_VIEW));
  }

  @Test
  public void serializeViewWithIntervalView() throws Exception {
    assertEquals(VIEW_WITH_INTERVAL_VIEW_PROTO, MonitoringUtil.serializeView(INTERVAL_VIEW));
  }

  @Test
  public void serializeDistributionView() throws Exception {
    assertEquals(
        DISTRIBUTION_VIEW_PROTO, MonitoringUtil.serializeDistributionView(DISTRIBUTION_VIEW));
  }

  @Test
  public void serializeTimestamp() throws Exception {
    assertEquals(START_TIMESTAMP_PROTO, MonitoringUtil.serializeTimestamp(START_TIMESTAMP));
  }

  @Test
  public void serializeDistributionAggregation() throws Exception {
    assertEquals(
        DISTRIBUTION_AGG_PROTO, MonitoringUtil.serializeDistributionAggregation(DISTRIBUTION_AGG));
  }

  @Test
  public void serializeRange() throws Exception {
    assertEquals(RANGE_PROTO, MonitoringUtil.serializeRange(RANGE));
  }

  @Test
  public void serializeTag() throws Exception {
    assertEquals(TAG_PROTO, MonitoringUtil.serializeTag(TAG));
  }

  @Test
  public void serializeIntervalView() throws Exception {
    assertEquals(INTERVAL_VIEW_PROTO, MonitoringUtil.serializeIntervalView(INTERVAL_VIEW));
  }

  @Test
  public void serializeIntervalAggregation() throws Exception {
    assertEquals(INTERVAL_AGG_PROTO, MonitoringUtil.serializeIntervalAggregation(INTERVAL_AGG));
  }

  @Test
  public void serializeInterval() throws Exception {
    assertEquals(INTERVAL_PROTO, MonitoringUtil.serializeInterval(INTERVAL));
  }

  private static final int UNIT_POWER = -3;
  private static final MeasurementUnit MEASUREMENT_UNIT =
      MeasurementUnit.create(
          UNIT_POWER,
          Arrays.asList(BasicUnit.SCALAR),
          Arrays.asList(BasicUnit.SECONDS, BasicUnit.SECONDS));
  private static final CensusProto.MeasurementDescriptor.MeasurementUnit MEASUREMENT_UNIT_PROTO =
      CensusProto.MeasurementDescriptor.MeasurementUnit.newBuilder()
          .setPower10(UNIT_POWER)
          .addNumerators(MonitoringUtil.serializeBasicUnit(BasicUnit.SCALAR))
          .addDenominators(MonitoringUtil.serializeBasicUnit(BasicUnit.SECONDS))
          .addDenominators(MonitoringUtil.serializeBasicUnit(BasicUnit.SECONDS))
          .build();

  private static final String MEASUREMENT_DESC_NAME = "measurement descriptor name";
  private static final String MEASUREMENT_DESC_DESCRIPTION = "measurement descriptor description";
  private static final MeasurementDescriptor measurementDescriptor =
      MeasurementDescriptor.create(
          MEASUREMENT_DESC_NAME, MEASUREMENT_DESC_DESCRIPTION, MEASUREMENT_UNIT);
  private static final CensusProto.MeasurementDescriptor MEASUREMENT_DESC_PROTO =
      CensusProto.MeasurementDescriptor.newBuilder()
          .setName(MEASUREMENT_DESC_NAME)
          .setDescription(MEASUREMENT_DESC_DESCRIPTION)
          .setUnit(MEASUREMENT_UNIT_PROTO)
          .build();

  private static final long START_SECONDS = 1L;
  private static final int START_NANOS = 1;
  private static final long END_SECONDS = 100000L;
  private static final int END_NANOS = 9999;
  private static final Timestamp START_TIMESTAMP = Timestamp.create(START_SECONDS, START_NANOS);
  private static final Timestamp END_TIMESTAMP = Timestamp.create(END_SECONDS, END_NANOS);
  private static final CensusProto.Timestamp START_TIMESTAMP_PROTO =
      CensusProto.Timestamp.newBuilder().setSeconds(START_SECONDS).setNanos(START_NANOS).build();
  // TODO(ericgribkoff) Re-enable once getter methods are public in instrumentation.
  //private static final CensusProto.Timestamp END_TIMESTAMP_PROTO =
  //    CensusProto.Timestamp.newBuilder().setSeconds(END_SECONDS).setNanos(END_NANOS).build();

  private static final String TAG_KEY = "tag key";
  private static final String TAG_VALUE = "tag value";
  private static final Tag TAG = Tag.create(TagKey.create(TAG_KEY), TagValue.create(TAG_VALUE));
  private static final CensusProto.Tag TAG_PROTO =
      CensusProto.Tag.newBuilder().setKey(TAG_KEY).setValue(TAG_VALUE).build();

  private static final double RANGE_MIN = 0.1;
  private static final double RANGE_MAX = 999.9;
  private static final Range RANGE = Range.create(RANGE_MIN, RANGE_MAX);
  private static final CensusProto.DistributionAggregation.Range RANGE_PROTO =
      CensusProto.DistributionAggregation.Range.newBuilder()
          .setMin(RANGE_MIN)
          .setMax(RANGE_MAX)
          .build();

  private static final long DISTRIBUTION_AGG_COUNT = 100L;
  private static final double DISTRIBUTION_AGG_MEAN = 55.1;
  private static final double DISTRIBUTION_AGG_SUM = 4098.5;
  private static final long BUCKET_COUNT = 11L;
  private static final DistributionAggregation DISTRIBUTION_AGG =
      DistributionAggregation.create(
          DISTRIBUTION_AGG_COUNT,
          DISTRIBUTION_AGG_MEAN,
          DISTRIBUTION_AGG_SUM,
          RANGE,
          Arrays.asList(TAG),
          Arrays.asList(BUCKET_COUNT));
  private static final CensusProto.DistributionAggregation DISTRIBUTION_AGG_PROTO =
      CensusProto.DistributionAggregation.newBuilder()
          .setCount(DISTRIBUTION_AGG_COUNT)
          .setMean(DISTRIBUTION_AGG_MEAN)
          .setSum(DISTRIBUTION_AGG_SUM)
          .setRange(RANGE_PROTO)
          .addAllBucketCounts(Arrays.asList(BUCKET_COUNT))
          .addTags(TAG_PROTO)
          .build();

  private static final double BUCKET_BOUNDARY = 14.0;
  private static final DistributionAggregationDescriptor DISTRIBUTION_AGG_DESC =
      DistributionAggregationDescriptor.create(Arrays.asList(BUCKET_BOUNDARY));
  private static final CensusProto.DistributionAggregationDescriptor DISTRIBUTION_AGG_DESC_PROTO =
      CensusProto.DistributionAggregationDescriptor.newBuilder()
          .addBucketBounds(BUCKET_BOUNDARY)
          .build();

  private static final String DISTRIBUTION_VIEW_NAME = "distribution view name";
  private static final String DISTRIBUTION_VIEW_DESCRIPTION = "distribution view description";
  private static final DistributionViewDescriptor DISTRIBUTION_VIEW_DESC =
      DistributionViewDescriptor.create(
          DISTRIBUTION_VIEW_NAME,
          DISTRIBUTION_VIEW_DESCRIPTION,
          measurementDescriptor,
          DISTRIBUTION_AGG_DESC,
          Arrays.asList(TagKey.create(TAG_KEY)));
  private static final CensusProto.ViewDescriptor DISTRIBUTION_VIEW_DESC_PROTO =
      CensusProto.ViewDescriptor.newBuilder()
          .setName(DISTRIBUTION_VIEW_NAME)
          .setDescription(DISTRIBUTION_VIEW_DESCRIPTION)
          .setMeasurementDescriptorName(MEASUREMENT_DESC_NAME)
          .setDistributionAggregation(DISTRIBUTION_AGG_DESC_PROTO)
          .addTagKeys(TAG_KEY)
          .build();

  static final DistributionView DISTRIBUTION_VIEW =
      DistributionView.create(
          DISTRIBUTION_VIEW_DESC, Arrays.asList(DISTRIBUTION_AGG), START_TIMESTAMP, END_TIMESTAMP);
  private static final CensusProto.DistributionView DISTRIBUTION_VIEW_PROTO =
      CensusProto.DistributionView.newBuilder()
          .addAggregations(DISTRIBUTION_AGG_PROTO)
          // TODO(ericgribkoff) Re-enable once getter methods are public in instrumentation.
          //.setStart(START_TIMESTAMP_PROTO)
          //.setEnd(END_TIMESTAMP_PROTO)
          .build();
  private static final CensusProto.View VIEW_WITH_DISTRIBUTION_VIEW_PROTO =
      CensusProto.View.newBuilder()
          .setViewName(DISTRIBUTION_VIEW_NAME)
          .setDistributionView(DISTRIBUTION_VIEW_PROTO)
          .build();
  private static final StatsResponse DISTRIBUTION_STATS_RESPONSE_PROTO =
      StatsResponse.newBuilder()
          .setMeasurementDescriptor(MEASUREMENT_DESC_PROTO)
          .setViewDescriptor(DISTRIBUTION_VIEW_DESC_PROTO)
          .setView(VIEW_WITH_DISTRIBUTION_VIEW_PROTO)
          .build();

  private static final long DURATION_SECONDS = 100L;
  private static final int DURATION_NANOS = 9999;
  private static final Duration DURATION = Duration.create(DURATION_SECONDS, DURATION_NANOS);
  private static final CensusProto.Duration DURATION_PROTO =
      CensusProto.Duration.newBuilder()
          .setSeconds(DURATION_SECONDS)
          .setNanos(DURATION_NANOS)
          .build();

  private static final int NUM_SUB_INTERVALS = 2;
  private static final IntervalAggregationDescriptor INTERVAL_AGG_DESC =
      IntervalAggregationDescriptor.create(NUM_SUB_INTERVALS, Arrays.asList(DURATION));
  private static final CensusProto.IntervalAggregationDescriptor INTERVAL_AGG_DESC_PROTO =
      CensusProto.IntervalAggregationDescriptor.newBuilder()
          .setNSubIntervals(NUM_SUB_INTERVALS)
          .addIntervalSizes(DURATION_PROTO)
          .build();

  private static final String INTERVAL_VIEW_NAME = "interval view name";
  private static final String INTERVAL_VIEW_DESCRIPTION = "interval view description";
  private static final IntervalViewDescriptor INTERVAL_VIEW_DESC =
      IntervalViewDescriptor.create(
          INTERVAL_VIEW_NAME,
          INTERVAL_VIEW_DESCRIPTION,
          measurementDescriptor,
          INTERVAL_AGG_DESC,
          Arrays.asList(TagKey.create(TAG_KEY)));
  private static final CensusProto.ViewDescriptor INTERVAL_VIEW_DESC_PROTO =
      CensusProto.ViewDescriptor.newBuilder()
          .setName(INTERVAL_VIEW_NAME)
          .setDescription(INTERVAL_VIEW_DESCRIPTION)
          .setMeasurementDescriptorName(MEASUREMENT_DESC_NAME)
          .setIntervalAggregation(INTERVAL_AGG_DESC_PROTO)
          .addTagKeys(TAG_KEY)
          .build();

  private static final double INTERVAL_COUNT = 6.0;
  private static final double INTERVAL_SUM = 98.5;
  private static final Interval INTERVAL = Interval.create(DURATION, INTERVAL_COUNT, INTERVAL_SUM);
  private static final CensusProto.IntervalAggregation.Interval INTERVAL_PROTO =
      CensusProto.IntervalAggregation.Interval.newBuilder()
          .setIntervalSize(DURATION_PROTO)
          .setCount(INTERVAL_COUNT)
          .setSum(INTERVAL_SUM)
          .build();

  private static final IntervalAggregation INTERVAL_AGG =
      IntervalAggregation.create(Arrays.asList(TAG), Arrays.asList(INTERVAL));
  private static final CensusProto.IntervalAggregation INTERVAL_AGG_PROTO =
      CensusProto.IntervalAggregation.newBuilder()
          .addIntervals(INTERVAL_PROTO)
          .addTags(TAG_PROTO)
          .build();

  static final IntervalView INTERVAL_VIEW =
      IntervalView.create(INTERVAL_VIEW_DESC, Arrays.asList(INTERVAL_AGG));
  private static final CensusProto.IntervalView INTERVAL_VIEW_PROTO =
      CensusProto.IntervalView.newBuilder().addAggregations(INTERVAL_AGG_PROTO).build();
  private static final CensusProto.View VIEW_WITH_INTERVAL_VIEW_PROTO =
      CensusProto.View.newBuilder()
          .setViewName(INTERVAL_VIEW_NAME)
          .setIntervalView(INTERVAL_VIEW_PROTO)
          .build();
  private static final StatsResponse INTERVAL_STATS_RESPONSE_PROTO =
      StatsResponse.newBuilder()
          .setMeasurementDescriptor(MEASUREMENT_DESC_PROTO)
          .setViewDescriptor(INTERVAL_VIEW_DESC_PROTO)
          .setView(VIEW_WITH_INTERVAL_VIEW_PROTO)
          .build();
}
