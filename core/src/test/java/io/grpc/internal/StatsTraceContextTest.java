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

package io.grpc.internal;

import static java.util.concurrent.TimeUnit.MILLISECONDS;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;

import com.google.instrumentation.stats.RpcConstants;
import com.google.instrumentation.stats.StatsContext;
import com.google.instrumentation.stats.StatsContextFactory;
import com.google.instrumentation.stats.TagValue;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.Status;
import io.grpc.internal.testing.StatsTestUtils;
import io.grpc.internal.testing.StatsTestUtils.FakeStatsContextFactory;
import org.junit.After;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Test for {@link StatsTraceContext}.
 */
@RunWith(JUnit4.class)
public class StatsTraceContextTest {
  private FakeClock fakeClock = new FakeClock();
  private FakeStatsContextFactory statsCtxFactory = new FakeStatsContextFactory();

  @After
  public void allRecordsVerified() {
    assertNull(statsCtxFactory.pollRecord());
  }

  @Test
  public void clientBasic() {
    String methodName = MethodDescriptor.generateFullMethodName("Service1", "method1");
    StatsTraceContext ctx = StatsTraceContext.newClientContextForTesting(
        methodName, statsCtxFactory, statsCtxFactory.getDefault(),
        fakeClock.getStopwatchSupplier());
    fakeClock.forwardTime(30, MILLISECONDS);
    ctx.clientHeadersSent();

    fakeClock.forwardTime(100, MILLISECONDS);
    ctx.wireBytesSent(1028);
    ctx.uncompressedBytesSent(1128);

    fakeClock.forwardTime(16, MILLISECONDS);
    ctx.wireBytesReceived(33);
    ctx.uncompressedBytesReceived(67);
    ctx.wireBytesSent(99);
    ctx.uncompressedBytesSent(865);

    fakeClock.forwardTime(24, MILLISECONDS);
    ctx.wireBytesReceived(154);
    ctx.uncompressedBytesReceived(552);
    ctx.callEnded(Status.OK);

    StatsTestUtils.MetricsRecord record = statsCtxFactory.pollRecord();
    assertNotNull(record);
    assertNoServerContent(record);
    TagValue methodTag = record.tags.get(RpcConstants.RPC_CLIENT_METHOD);
    assertEquals(methodName, methodTag.toString());
    TagValue statusTag = record.tags.get(RpcConstants.RPC_STATUS);
    assertEquals(Status.Code.OK.toString(), statusTag.toString());
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_ERROR_COUNT));
    assertEquals(1028 + 99, record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_REQUEST_BYTES));
    assertEquals(1128 + 865,
        record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES));
    assertEquals(33 + 154, record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_RESPONSE_BYTES));
    assertEquals(67 + 552,
        record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES));
    assertEquals(30 + 100 + 16 + 24,
        record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY));
    assertEquals(100 + 16 + 24,
        record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_SERVER_ELAPSED_TIME));
  }

  @Test
  public void clientNotSent() {
    String methodName = MethodDescriptor.generateFullMethodName("Service1", "method2");
    StatsTraceContext ctx = StatsTraceContext.newClientContextForTesting(
        methodName, statsCtxFactory, statsCtxFactory.getDefault(),
        fakeClock.getStopwatchSupplier());
    fakeClock.forwardTime(3000, MILLISECONDS);
    ctx.callEnded(Status.DEADLINE_EXCEEDED.withDescription("3 seconds"));

    StatsTestUtils.MetricsRecord record = statsCtxFactory.pollRecord();
    assertNotNull(record);
    assertNoServerContent(record);
    TagValue methodTag = record.tags.get(RpcConstants.RPC_CLIENT_METHOD);
    assertEquals(methodName, methodTag.toString());
    TagValue statusTag = record.tags.get(RpcConstants.RPC_STATUS);
    assertEquals(Status.Code.DEADLINE_EXCEEDED.toString(), statusTag.toString());
    assertEquals(1, record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_ERROR_COUNT));
    assertEquals(0, record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_REQUEST_BYTES));
    assertEquals(0,
        record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES));
    assertEquals(0, record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_RESPONSE_BYTES));
    assertEquals(0,
        record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES));
    assertEquals(3000, record.getMetricAsLongOrFail(RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_SERVER_ELAPSED_TIME));
  }

  /**
   * Tags that are propagated by the {@link StatsContextFactory} are properly propagated via
   * the headers.
   */
  @Test
  public void tagPropagation() {
    String methodName = MethodDescriptor.generateFullMethodName("Service1", "method3");

    // EXTRA_TAG is propagated by the FakeStatsContextFactory. Note that not all tags are
    // propagated.  The StatsContextFactory decides which tags are to propagated.  gRPC facilitates
    // the propagation by putting them in the headers.
    StatsContext parentCtx = statsCtxFactory.getDefault().with(
        StatsTestUtils.EXTRA_TAG, TagValue.create("extra-tag-value-897"));
    StatsTraceContext clientCtx = StatsTraceContext.newClientContextForTesting(
        methodName, statsCtxFactory, parentCtx, fakeClock.getStopwatchSupplier());
    Metadata headers = new Metadata();
    clientCtx.propagateToHeaders(headers);

    // The server gets the propagated tag from the headers, and puts it on the server-side
    // StatsContext.
    StatsTraceContext serverCtx = StatsTraceContext.newServerContext(
        methodName, statsCtxFactory, headers, fakeClock.getStopwatchSupplier());

    serverCtx.callEnded(Status.OK);
    clientCtx.callEnded(Status.OK);

    StatsTestUtils.MetricsRecord serverRecord = statsCtxFactory.pollRecord();
    assertNotNull(serverRecord);
    assertNoClientContent(serverRecord);
    TagValue serverMethodTag = serverRecord.tags.get(RpcConstants.RPC_SERVER_METHOD);
    assertEquals(methodName, serverMethodTag.toString());
    TagValue serverStatusTag = serverRecord.tags.get(RpcConstants.RPC_STATUS);
    assertEquals(Status.Code.OK.toString(), serverStatusTag.toString());
    assertNull(serverRecord.getMetric(RpcConstants.RPC_SERVER_ERROR_COUNT));
    TagValue serverPropagatedTag = serverRecord.tags.get(StatsTestUtils.EXTRA_TAG);
    assertEquals("extra-tag-value-897", serverPropagatedTag.toString());

    StatsTestUtils.MetricsRecord clientRecord = statsCtxFactory.pollRecord();
    assertNotNull(clientRecord);
    assertNoServerContent(clientRecord);
    TagValue clientMethodTag = clientRecord.tags.get(RpcConstants.RPC_CLIENT_METHOD);
    assertEquals(methodName, clientMethodTag.toString());
    TagValue clientStatusTag = clientRecord.tags.get(RpcConstants.RPC_STATUS);
    assertEquals(Status.Code.OK.toString(), clientStatusTag.toString());
    assertNull(clientRecord.getMetric(RpcConstants.RPC_CLIENT_ERROR_COUNT));
    TagValue clientPropagatedTag = clientRecord.tags.get(StatsTestUtils.EXTRA_TAG);
    assertEquals("extra-tag-value-897", clientPropagatedTag.toString());
  }

  @Test
  public void serverBasic() {
    String methodName = MethodDescriptor.generateFullMethodName("Service1", "method4");
    StatsTraceContext ctx = StatsTraceContext.newServerContext(
        methodName, statsCtxFactory, new Metadata(), fakeClock.getStopwatchSupplier());
    ctx.wireBytesReceived(34);
    ctx.uncompressedBytesReceived(67);

    fakeClock.forwardTime(100, MILLISECONDS);
    ctx.wireBytesSent(1028);
    ctx.uncompressedBytesSent(1128);

    fakeClock.forwardTime(16, MILLISECONDS);
    ctx.wireBytesReceived(154);
    ctx.uncompressedBytesReceived(552);
    ctx.wireBytesSent(99);
    ctx.uncompressedBytesSent(865);

    fakeClock.forwardTime(24, MILLISECONDS);
    ctx.callEnded(Status.CANCELLED);

    StatsTestUtils.MetricsRecord record = statsCtxFactory.pollRecord();
    assertNotNull(record);
    assertNoClientContent(record);
    TagValue methodTag = record.tags.get(RpcConstants.RPC_SERVER_METHOD);
    assertEquals(methodName, methodTag.toString());
    TagValue statusTag = record.tags.get(RpcConstants.RPC_STATUS);
    assertEquals(Status.Code.CANCELLED.toString(), statusTag.toString());
    assertEquals(1, record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_ERROR_COUNT));
    assertEquals(1028 + 99, record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_RESPONSE_BYTES));
    assertEquals(1128 + 865,
        record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_UNCOMPRESSED_RESPONSE_BYTES));
    assertEquals(34 + 154, record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_REQUEST_BYTES));
    assertEquals(67 + 552,
        record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_UNCOMPRESSED_REQUEST_BYTES));
    assertEquals(100 + 16 + 24,
        record.getMetricAsLongOrFail(RpcConstants.RPC_SERVER_SERVER_LATENCY));
  }

  private static void assertNoServerContent(StatsTestUtils.MetricsRecord record) {
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_ERROR_COUNT));
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_REQUEST_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_RESPONSE_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_SERVER_ELAPSED_TIME));
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_SERVER_LATENCY));
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_UNCOMPRESSED_REQUEST_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_SERVER_UNCOMPRESSED_RESPONSE_BYTES));
  }

  private static void assertNoClientContent(StatsTestUtils.MetricsRecord record) {
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_ERROR_COUNT));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_REQUEST_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_RESPONSE_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_ROUNDTRIP_LATENCY));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_SERVER_ELAPSED_TIME));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_UNCOMPRESSED_REQUEST_BYTES));
    assertNull(record.getMetric(RpcConstants.RPC_CLIENT_UNCOMPRESSED_RESPONSE_BYTES));
  }
}
