package com.google.net.stubby.stub;

import static org.junit.Assert.assertEquals;

import com.google.net.stubby.Call;
import com.google.net.stubby.Channel;
import com.google.net.stubby.MethodDescriptor;
import com.google.net.stubby.testing.integration.TestServiceGrpc;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

import java.util.concurrent.TimeUnit;

/**
 * Tests for stub reconfiguration
 */
@RunWith(JUnit4.class)
public class StubConfigTest {

  @Test
  public void testConfigureTimeout() {
    // Create a default stub
    TestServiceGrpc.TestServiceBlockingStub stub =
        TestServiceGrpc.newBlockingStub(new FakeChannel());
    assertEquals(TimeUnit.SECONDS.toMicros(1),
        stub.getServiceDescriptor().fullDuplexCall.getTimeout());
    // Reconfigure it
    stub = stub.configureNewStub()
        .setTimeout(2, TimeUnit.SECONDS)
        .build();
    // New altered config
    assertEquals(TimeUnit.SECONDS.toMicros(2),
        stub.getServiceDescriptor().fullDuplexCall.getTimeout());
    // Default config unchanged
    assertEquals(TimeUnit.SECONDS.toMicros(1),
        TestServiceGrpc.CONFIG.fullDuplexCall.getTimeout());
  }


  private static class FakeChannel implements Channel {
    @Override
    public <ReqT, RespT> Call<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method) {
      return null;
    }
  }
}
