package com.google.net.stubby.newtransport;

import static org.junit.Assert.assertEquals;
import static org.mockito.Mockito.when;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.stubbing.OngoingStubbing;


/**
 * Tests for {@link AbstractBuffer}.
 */
@RunWith(JUnit4.class)
public class AbstractBufferTest {

  @Mock
  private AbstractBuffer buffer;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void readUnsignedShortShouldSucceed() {
    mockBytes(0xFF, 0xEE);
    assertEquals(0xFFEE, buffer.readUnsignedShort());
  }

  @Test
  public void readUnsignedMediumShouldSucceed() {
    mockBytes(0xFF, 0xEE, 0xDD);
    assertEquals(0xFFEEDD, buffer.readUnsignedMedium());
  }

  @Test
  public void readPositiveIntShouldSucceed() {
    mockBytes(0x7F, 0xEE, 0xDD, 0xCC);
    assertEquals(0x7FEEDDCC, buffer.readInt());
  }

  @Test
  public void readNegativeIntShouldSucceed() {
    mockBytes(0xFF, 0xEE, 0xDD, 0xCC);
    assertEquals(0xFFEEDDCC, buffer.readInt());
  }

  private void mockBytes(int... bytes) {
    when(buffer.readableBytes()).thenReturn(bytes.length);
    OngoingStubbing<Integer> stub = when(buffer.readUnsignedByte());
    for (int b : bytes) {
      stub = stub.thenReturn(b);
    }
  }
}
