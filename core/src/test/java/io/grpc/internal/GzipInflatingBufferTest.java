/*
 * Copyright 2017 The gRPC Authors
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

package io.grpc.internal;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import com.google.common.io.ByteStreams;
import java.io.ByteArrayOutputStream;
import java.io.OutputStream;
import java.util.Arrays;
import java.util.zip.CRC32;
import java.util.zip.DataFormatException;
import java.util.zip.GZIPOutputStream;
import java.util.zip.ZipException;
import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link GzipInflatingBuffer}. */
@RunWith(JUnit4.class)
public class GzipInflatingBufferTest {
  private static final String UNCOMPRESSABLE_FILE = "/io/grpc/internal/uncompressable.bin";

  private static final int GZIP_HEADER_MIN_SIZE = 10;
  private static final int GZIP_TRAILER_SIZE = 8;
  private static final int GZIP_HEADER_FLAG_INDEX = 3;

  public static final int GZIP_MAGIC = 0x8b1f;

  private static final int FTEXT = 1;
  private static final int FHCRC = 2;
  private static final int FEXTRA = 4;
  private static final int FNAME = 8;
  private static final int FCOMMENT = 16;

  private static final int TRUNCATED_DATA_SIZE = 10;

  private byte[] originalData;
  private byte[] gzippedData;
  private byte[] gzipHeader;
  private byte[] deflatedBytes;
  private byte[] gzipTrailer;
  private byte[] truncatedData;
  private byte[] gzippedTruncatedData;

  private GzipInflatingBuffer gzipInflatingBuffer;

  @Before
  public void setUp() {
    gzipInflatingBuffer = new GzipInflatingBuffer();
    try {
      originalData = ByteStreams.toByteArray(getClass().getResourceAsStream(UNCOMPRESSABLE_FILE));
      truncatedData = Arrays.copyOf(originalData, TRUNCATED_DATA_SIZE);

      ByteArrayOutputStream gzippedOutputStream = new ByteArrayOutputStream();
      OutputStream gzippingOutputStream = new GZIPOutputStream(gzippedOutputStream);
      gzippingOutputStream.write(originalData);
      gzippingOutputStream.close();
      gzippedData = gzippedOutputStream.toByteArray();
      gzippedOutputStream.close();

      gzipHeader = Arrays.copyOf(gzippedData, GZIP_HEADER_MIN_SIZE);
      deflatedBytes =
          Arrays.copyOfRange(
              gzippedData, GZIP_HEADER_MIN_SIZE, gzippedData.length - GZIP_TRAILER_SIZE);
      gzipTrailer =
          Arrays.copyOfRange(
              gzippedData, gzippedData.length - GZIP_TRAILER_SIZE, gzippedData.length);

      ByteArrayOutputStream truncatedGzippedOutputStream = new ByteArrayOutputStream();
      OutputStream smallerGzipCompressingStream =
          new GZIPOutputStream(truncatedGzippedOutputStream);
      smallerGzipCompressingStream.write(truncatedData);
      smallerGzipCompressingStream.close();
      gzippedTruncatedData = truncatedGzippedOutputStream.toByteArray();
      truncatedGzippedOutputStream.close();
    } catch (Exception e) {
      throw new RuntimeException("Failed to set up compressed data", e);
    }
  }

  @After
  public void tearDown() {
    gzipInflatingBuffer.close();
  }

  @Test
  public void gzipInflateWorks() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(gzippedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void splitGzipStreamWorks() throws Exception {
    int initialBytes = 100;
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData, 0, initialBytes));

    byte[] b = new byte[originalData.length];
    int n = gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
    assertTrue("inflated bytes expected", n > 0);
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());
    assertEquals(initialBytes, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(
        ReadableBuffers.wrap(gzippedData, initialBytes, gzippedData.length - initialBytes));
    int bytesRemaining = originalData.length - n;
    assertEquals(bytesRemaining, gzipInflatingBuffer.inflateBytes(b, n, bytesRemaining));
    assertEquals(gzippedData.length - initialBytes, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void inflateBytesObeysOffsetAndLength() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    int offset = 10;
    int length = 100;
    byte[] b = new byte[offset + length + offset];
    assertEquals(length, gzipInflatingBuffer.inflateBytes(b, offset, length));
    assertTrue(
        "bytes written before offset",
        Arrays.equals(new byte[offset], Arrays.copyOfRange(b, 0, offset)));
    assertTrue(
        "inflated data does not match",
        Arrays.equals(
            Arrays.copyOfRange(originalData, 0, length),
            Arrays.copyOfRange(b, offset, offset + length)));
    assertTrue(
        "bytes written beyond length",
        Arrays.equals(
            new byte[offset], Arrays.copyOfRange(b, offset + length, offset + length + offset)));
  }

  @Test
  public void concatenatedStreamsWorks() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedTruncatedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedTruncatedData));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(gzippedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));

    assertEquals(
        truncatedData.length, gzipInflatingBuffer.inflateBytes(b, 0, truncatedData.length));
    assertEquals(gzippedTruncatedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));

    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(gzippedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));

    assertEquals(
        truncatedData.length, gzipInflatingBuffer.inflateBytes(b, 0, truncatedData.length));
    assertEquals(gzippedTruncatedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void requestingTooManyBytesStillReturnsEndOfBlock() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    int len = 2 * originalData.length;
    byte[] b = new byte[len];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, len));
    assertEquals(gzippedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue(gzipInflatingBuffer.isStalled());
    assertTrue(
        "inflated data does not match",
        Arrays.equals(originalData, Arrays.copyOf(b, originalData.length)));
  }

  @Test
  public void closeStopsDecompression() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    byte[] b = new byte[1];
    gzipInflatingBuffer.inflateBytes(b, 0, 1);
    gzipInflatingBuffer.close();
    try {
      gzipInflatingBuffer.inflateBytes(b, 0, 1);
      fail("Expected IllegalStateException");
    } catch (IllegalStateException expectedException) {
      assertEquals("GzipInflatingBuffer is closed", expectedException.getMessage());
    }
  }

  @Test
  public void isStalledReturnsTrueAtEndOfStream() throws Exception {
    int bytesToWithhold = 10;

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    byte[] b = new byte[originalData.length];
    gzipInflatingBuffer.inflateBytes(b, 0, originalData.length - bytesToWithhold);
    assertFalse("gzipInflatingBuffer is stalled", gzipInflatingBuffer.isStalled());

    gzipInflatingBuffer.inflateBytes(b, originalData.length - bytesToWithhold, bytesToWithhold);
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());
  }

  @Test
  public void isStalledReturnsFalseBetweenStreams() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
    assertFalse("gzipInflatingBuffer is stalled", gzipInflatingBuffer.isStalled());

    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());
  }

  @Test
  public void isStalledReturnsFalseBetweenSmallStreams() throws Exception {
    // Use small streams to make sure that they all fit in the inflater buffer
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedTruncatedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedTruncatedData));

    byte[] b = new byte[truncatedData.length];
    assertEquals(
        truncatedData.length, gzipInflatingBuffer.inflateBytes(b, 0, truncatedData.length));
    assertTrue("inflated data does not match", Arrays.equals(truncatedData, b));
    assertFalse("gzipInflatingBuffer is stalled", gzipInflatingBuffer.isStalled());

    assertEquals(
        truncatedData.length, gzipInflatingBuffer.inflateBytes(b, 0, truncatedData.length));
    assertTrue("inflated data does not match", Arrays.equals(truncatedData, b));
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());
  }

  @Test
  public void isStalledReturnsTrueWithPartialNextHeaderAvailable() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedTruncatedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(new byte[1]));

    byte[] b = new byte[truncatedData.length];
    assertEquals(
        truncatedData.length, gzipInflatingBuffer.inflateBytes(b, 0, truncatedData.length));
    assertTrue("inflated data does not match", Arrays.equals(truncatedData, b));
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());
    assertTrue("partial data expected", gzipInflatingBuffer.hasPartialData());
  }

  @Test
  public void isStalledWorksWithAllHeaderFlags() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] =
        (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FTEXT | FHCRC | FEXTRA | FNAME | FCOMMENT);
    int len = 1025;
    byte[] fExtraLen = {(byte) len, (byte) (len >> 8)};
    byte[] fExtra = new byte[len];
    byte[] zeroTerminatedBytes = new byte[len];
    for (int i = 0; i < len - 1; i++) {
      zeroTerminatedBytes[i] = 1;
    }
    ByteArrayOutputStream newHeader = new ByteArrayOutputStream();
    newHeader.write(gzipHeader);
    newHeader.write(fExtraLen);
    newHeader.write(fExtra);
    newHeader.write(zeroTerminatedBytes); // FNAME
    newHeader.write(zeroTerminatedBytes); // FCOMMENT
    byte[] headerCrc16 = getHeaderCrc16Bytes(newHeader.toByteArray());

    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());

    addInTwoChunksAndVerifyIsStalled(gzipHeader);
    addInTwoChunksAndVerifyIsStalled(fExtraLen);
    addInTwoChunksAndVerifyIsStalled(fExtra);
    addInTwoChunksAndVerifyIsStalled(zeroTerminatedBytes);
    addInTwoChunksAndVerifyIsStalled(zeroTerminatedBytes);
    addInTwoChunksAndVerifyIsStalled(headerCrc16);

    byte[] b = new byte[originalData.length];
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));

    addInTwoChunksAndVerifyIsStalled(gzipTrailer);
  }

  @Test
  public void hasPartialData() throws Exception {
    assertFalse("no partial data expected", gzipInflatingBuffer.hasPartialData());
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(new byte[1]));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
    assertTrue("partial data expected", gzipInflatingBuffer.hasPartialData());
  }

  @Test
  public void hasPartialDataWithoutGzipTrailer() throws Exception {
    assertFalse("no partial data expected", gzipInflatingBuffer.hasPartialData());
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
    assertTrue("partial data expected", gzipInflatingBuffer.hasPartialData());
  }

  @Test
  public void inflatingCompleteGzipStreamConsumesTrailer() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
    assertFalse("no partial data expected", gzipInflatingBuffer.hasPartialData());
  }

  @Test
  public void bytesConsumedForPartiallyInflatedBlock() throws Exception {
    int bytesToWithhold = 1;
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedTruncatedData));

    byte[] b = new byte[truncatedData.length];
    assertEquals(
        truncatedData.length - bytesToWithhold,
        gzipInflatingBuffer.inflateBytes(b, 0, truncatedData.length - bytesToWithhold));
    assertEquals(
        gzippedTruncatedData.length - bytesToWithhold - GZIP_TRAILER_SIZE,
        gzipInflatingBuffer.getAndResetBytesConsumed());
    assertEquals(
        bytesToWithhold,
        gzipInflatingBuffer.inflateBytes(
            b, truncatedData.length - bytesToWithhold, bytesToWithhold));
    assertEquals(
        bytesToWithhold + GZIP_TRAILER_SIZE, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(truncatedData, b));
  }

  @Test
  public void getAndResetCompressedBytesConsumedReportsHeaderFlagBytes() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] =
        (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FTEXT | FHCRC | FEXTRA | FNAME | FCOMMENT);
    int len = 1025;
    byte[] fExtraLen = {(byte) len, (byte) (len >> 8)};
    byte[] fExtra = new byte[len];
    byte[] zeroTerminatedBytes = new byte[len];
    for (int i = 0; i < len - 1; i++) {
      zeroTerminatedBytes[i] = 1;
    }
    ByteArrayOutputStream newHeader = new ByteArrayOutputStream();
    newHeader.write(gzipHeader);
    newHeader.write(fExtraLen);
    newHeader.write(fExtra);
    newHeader.write(zeroTerminatedBytes); // FNAME
    newHeader.write(zeroTerminatedBytes); // FCOMMENT
    byte[] headerCrc16 = getHeaderCrc16Bytes(newHeader.toByteArray());

    byte[] b = new byte[originalData.length];
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(0, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(gzipHeader.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(fExtraLen));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(fExtraLen.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(fExtra));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(fExtra.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(zeroTerminatedBytes));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(zeroTerminatedBytes.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(zeroTerminatedBytes));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(zeroTerminatedBytes.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(headerCrc16));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(headerCrc16.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(deflatedBytes.length, gzipInflatingBuffer.getAndResetBytesConsumed());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));
    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertEquals(gzipTrailer.length, gzipInflatingBuffer.getAndResetBytesConsumed());
  }

  @Test
  public void getAndResetDeflatedBytesConsumedExcludesGzipMetadata() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzippedData));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(
        gzippedData.length - GZIP_HEADER_MIN_SIZE - GZIP_TRAILER_SIZE,
        gzipInflatingBuffer.getAndResetDeflatedBytesConsumed());
  }

  @Test
  public void wrongHeaderMagicShouldFail() throws Exception {
    gzipHeader[1] = (byte) ~(GZIP_MAGIC >> 8);
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    try {
      byte[] b = new byte[1];
      gzipInflatingBuffer.inflateBytes(b, 0, 1);
      fail("Expected ZipException");
    } catch (ZipException expectedException) {
      assertEquals("Not in GZIP format", expectedException.getMessage());
    }
  }

  @Test
  public void wrongHeaderCompressionMethodShouldFail() throws Exception {
    gzipHeader[2] = 7; // Should be 8
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    try {
      byte[] b = new byte[1];
      gzipInflatingBuffer.inflateBytes(b, 0, 1);
      fail("Expected ZipException");
    } catch (ZipException expectedException) {
      assertEquals("Unsupported compression method", expectedException.getMessage());
    }
  }

  @Test
  public void allHeaderFlagsWork() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] =
        (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FTEXT | FHCRC | FEXTRA | FNAME | FCOMMENT);
    int len = 1025;
    byte[] fExtraLen = {(byte) len, (byte) (len >> 8)};
    byte[] fExtra = new byte[len];
    byte[] zeroTerminatedBytes = new byte[len];
    for (int i = 0; i < len - 1; i++) {
      zeroTerminatedBytes[i] = 1;
    }
    ByteArrayOutputStream newHeader = new ByteArrayOutputStream();
    newHeader.write(gzipHeader);
    newHeader.write(fExtraLen);
    newHeader.write(fExtra);
    newHeader.write(zeroTerminatedBytes); // FNAME
    newHeader.write(zeroTerminatedBytes); // FCOMMENT
    byte[] headerCrc16 = getHeaderCrc16Bytes(newHeader.toByteArray());
    newHeader.write(headerCrc16);

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(newHeader.toByteArray()));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerFTextFlagIsIgnored() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FTEXT);
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(gzippedData.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerFhcrcFlagWorks() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FHCRC);

    byte[] headerCrc16 = getHeaderCrc16Bytes(gzipHeader);

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(headerCrc16));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(
        gzippedData.length + headerCrc16.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerInvalidFhcrcFlagFails() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FHCRC);

    byte[] headerCrc16 = getHeaderCrc16Bytes(gzipHeader);
    headerCrc16[0] = (byte) ~headerCrc16[0];

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(headerCrc16));
    try {
      byte[] b = new byte[1];
      gzipInflatingBuffer.inflateBytes(b, 0, 1);
      fail("Expected ZipException");
    } catch (ZipException expectedException) {
      assertEquals("Corrupt GZIP header", expectedException.getMessage());
    }
  }

  @Test
  public void headerFExtraFlagWorks() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FEXTRA);

    int len = 1025;
    byte[] fExtraLen = {(byte) len, (byte) (len >> 8)};
    byte[] fExtra = new byte[len];

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(fExtraLen));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(fExtra));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(
        gzippedData.length + fExtraLen.length + fExtra.length,
        gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerFExtraFlagWithZeroLenWorks() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FEXTRA);
    byte[] fExtraLen = new byte[2];

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(fExtraLen));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(
        gzippedData.length + fExtraLen.length, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerFExtraFlagWithMissingExtraLenFails() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FEXTRA);

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected DataFormatException");
    } catch (DataFormatException expectedException) {
      assertTrue(
          "wrong exception message",
          expectedException.getMessage().startsWith("Inflater data format exception:"));
    }
  }

  @Test
  public void headerFExtraFlagWithMissingExtraBytesFails() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FEXTRA);

    int len = 5;
    byte[] fExtraLen = {(byte) len, (byte) (len >> 8)};

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(fExtraLen));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected DataFormatException");
    } catch (DataFormatException expectedException) {
      assertTrue(
          "wrong exception message",
          expectedException.getMessage().startsWith("Inflater data format exception:"));
    }
  }

  @Test
  public void headerFNameFlagWorks() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FNAME);
    int len = 1025;
    byte[] zeroTerminatedBytes = new byte[len];
    for (int i = 0; i < len - 1; i++) {
      zeroTerminatedBytes[i] = 1;
    }

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(zeroTerminatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(gzippedData.length + len, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerFNameFlagWithMissingBytesFail() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FNAME);
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected DataFormatException");
    } catch (DataFormatException expectedException) {
      assertTrue(
          "wrong exception message",
          expectedException.getMessage().startsWith("Inflater data format exception:"));
    }
  }

  @Test
  public void headerFCommentFlagWorks() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FCOMMENT);
    int len = 1025;
    byte[] zeroTerminatedBytes = new byte[len];
    for (int i = 0; i < len - 1; i++) {
      zeroTerminatedBytes[i] = 1;
    }

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(zeroTerminatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    byte[] b = new byte[originalData.length];
    assertEquals(originalData.length, gzipInflatingBuffer.inflateBytes(b, 0, originalData.length));
    assertEquals(gzippedData.length + len, gzipInflatingBuffer.getAndResetBytesConsumed());
    assertTrue("inflated data does not match", Arrays.equals(originalData, b));
  }

  @Test
  public void headerFCommentFlagWithMissingBytesFail() throws Exception {
    gzipHeader[GZIP_HEADER_FLAG_INDEX] = (byte) (gzipHeader[GZIP_HEADER_FLAG_INDEX] | FCOMMENT);

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));
    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected DataFormatException");
    } catch (DataFormatException expectedException) {
      assertTrue(
          "wrong exception message",
          expectedException.getMessage().startsWith("Inflater data format exception:"));
    }
  }

  @Test
  public void wrongTrailerCrcShouldFail() throws Exception {
    gzipTrailer[0] = (byte) ~gzipTrailer[0];
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected ZipException");
    } catch (ZipException expectedException) {
      assertEquals("Corrupt GZIP trailer", expectedException.getMessage());
    }
  }

  @Test
  public void wrongTrailerISizeShouldFail() throws Exception {
    gzipTrailer[GZIP_TRAILER_SIZE - 1] = (byte) ~gzipTrailer[GZIP_TRAILER_SIZE - 1];
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(deflatedBytes));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipTrailer));

    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected ZipException");
    } catch (ZipException expectedException) {
      assertEquals("Corrupt GZIP trailer", expectedException.getMessage());
    }
  }

  @Test
  public void invalidDeflateBlockShouldFail() throws Exception {
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(gzipHeader));
    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(new byte[10]));

    try {
      byte[] b = new byte[originalData.length];
      gzipInflatingBuffer.inflateBytes(b, 0, originalData.length);
      fail("Expected DataFormatException");
    } catch (DataFormatException expectedException) {
      assertTrue(
          "wrong exception message",
          expectedException.getMessage().startsWith("Inflater data format exception:"));
    }
  }

  private void addInTwoChunksAndVerifyIsStalled(byte[] input) throws Exception {
    byte[] b = new byte[1];

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(input, 0, input.length - 1));
    assertFalse("gzipInflatingBuffer is stalled", gzipInflatingBuffer.isStalled());

    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());

    gzipInflatingBuffer.addGzippedBytes(ReadableBuffers.wrap(input, input.length - 1, 1));
    assertFalse("gzipInflatingBuffer is stalled", gzipInflatingBuffer.isStalled());

    assertEquals(0, gzipInflatingBuffer.inflateBytes(b, 0, 1));
    assertTrue("gzipInflatingBuffer is not stalled", gzipInflatingBuffer.isStalled());
  }

  private byte[] getHeaderCrc16Bytes(byte[] headerBytes) {
    CRC32 crc = new CRC32();
    crc.update(headerBytes);
    byte[] headerCrc16 = {(byte) crc.getValue(), (byte) (crc.getValue() >> 8)};
    return headerCrc16;
  }
}
