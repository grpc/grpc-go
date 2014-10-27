package com.google.net.stubby.transport.netty;

import static com.google.common.base.Charsets.UTF_8;
import static org.junit.Assert.assertEquals;

import com.google.net.stubby.transport.Buffer;
import com.google.net.stubby.transport.BufferTestBase;

import io.netty.buffer.Unpooled;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link NettyBuffer}.
 */
@RunWith(JUnit4.class)
public class NettyBufferTest extends BufferTestBase {
  private NettyBuffer buffer;

  @Before
  public void setup() {
    buffer = new NettyBuffer(Unpooled.copiedBuffer(msg, UTF_8));
  }

  @Test
  public void closeShouldReleaseBuffer() {
    buffer.close();
    assertEquals(0, buffer.buffer().refCnt());
  }

  @Test
  public void closeMultipleTimesShouldReleaseBufferOnce() {
    buffer.close();
    buffer.close();
    assertEquals(0, buffer.buffer().refCnt());
  }

  @Override
  protected Buffer buffer() {
    return buffer;
  }
}
