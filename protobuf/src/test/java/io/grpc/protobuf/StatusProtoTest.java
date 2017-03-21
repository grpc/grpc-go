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

package io.grpc.protobuf;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.fail;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.StatusRuntimeException;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link StatusProto}. */
@RunWith(JUnit4.class)
public class StatusProtoTest {
  private Metadata metadata;

  @Before
  public void setup() {
    metadata = new Metadata();
    metadata.put(METADATA_KEY, METADATA_VALUE);
  }

  @Test
  public void toStatusRuntimeException() throws Exception {
    StatusRuntimeException sre = StatusProto.toStatusRuntimeException(STATUS_PROTO);
    com.google.rpc.Status extractedStatusProto = StatusProto.fromThrowable(sre);

    assertEquals(STATUS_PROTO.getCode(), sre.getStatus().getCode().value());
    assertEquals(STATUS_PROTO.getMessage(), sre.getStatus().getDescription());
    assertEquals(STATUS_PROTO, extractedStatusProto);
  }

  @Test
  public void toStatusRuntimeExceptionWithMetadata_shouldIncludeMetadata() throws Exception {
    StatusRuntimeException sre = StatusProto.toStatusRuntimeException(STATUS_PROTO, metadata);
    com.google.rpc.Status extractedStatusProto = StatusProto.fromThrowable(sre);

    assertEquals(STATUS_PROTO.getCode(), sre.getStatus().getCode().value());
    assertEquals(STATUS_PROTO.getMessage(), sre.getStatus().getDescription());
    assertEquals(STATUS_PROTO, extractedStatusProto);
    assertNotNull(sre.getTrailers());
    assertEquals(METADATA_VALUE, sre.getTrailers().get(METADATA_KEY));
  }

  @Test
  public void toStatusRuntimeExceptionWithMetadata_shouldThrowIfMetadataIsNull() throws Exception {
    try {
      StatusProto.toStatusRuntimeException(STATUS_PROTO, null);
      fail("NullPointerException expected");
    } catch (NullPointerException npe) {
      assertEquals("metadata must not be null", npe.getMessage());
    }
  }

  @Test
  public void toStatusRuntimeException_shouldThrowIfStatusCodeInvalid() throws Exception {
    try {
      StatusProto.toStatusRuntimeException(INVALID_STATUS_PROTO);
      fail("IllegalArgumentException expected");
    } catch (IllegalArgumentException expectedException) {
      assertEquals("invalid status code", expectedException.getMessage());
    }
  }

  @Test
  public void toStatusException() throws Exception {
    StatusException se = StatusProto.toStatusException(STATUS_PROTO);
    com.google.rpc.Status extractedStatusProto = StatusProto.fromThrowable(se);

    assertEquals(STATUS_PROTO.getCode(), se.getStatus().getCode().value());
    assertEquals(STATUS_PROTO.getMessage(), se.getStatus().getDescription());
    assertEquals(STATUS_PROTO, extractedStatusProto);
  }

  @Test
  public void toStatusExceptionWithMetadata_shouldIncludeMetadata() throws Exception {
    StatusException se = StatusProto.toStatusException(STATUS_PROTO, metadata);
    com.google.rpc.Status extractedStatusProto = StatusProto.fromThrowable(se);

    assertEquals(STATUS_PROTO.getCode(), se.getStatus().getCode().value());
    assertEquals(STATUS_PROTO.getMessage(), se.getStatus().getDescription());
    assertEquals(STATUS_PROTO, extractedStatusProto);
    assertNotNull(se.getTrailers());
    assertEquals(METADATA_VALUE, se.getTrailers().get(METADATA_KEY));
  }

  @Test
  public void toStatusExceptionWithMetadata_shouldThrowIfMetadataIsNull() throws Exception {
    try {
      StatusProto.toStatusException(STATUS_PROTO, null);
      fail("NullPointerException expected");
    } catch (NullPointerException npe) {
      assertEquals("metadata must not be null", npe.getMessage());
    }
  }

  @Test
  public void toStatusException_shouldThrowIfStatusCodeInvalid() throws Exception {
    try {
      StatusProto.toStatusException(INVALID_STATUS_PROTO);
      fail("IllegalArgumentException expected");
    } catch (IllegalArgumentException expectedException) {
      assertEquals("invalid status code", expectedException.getMessage());
    }
  }

  @Test
  public void fromThrowable_shouldReturnNullIfTrailersAreNull() {
    Status status = Status.fromCodeValue(0);

    assertNull(StatusProto.fromThrowable(status.asRuntimeException()));
    assertNull(StatusProto.fromThrowable(status.asException()));
  }

  @Test
  public void fromThrowableWithNestedStatusRuntimeException() {
    StatusRuntimeException sre = StatusProto.toStatusRuntimeException(STATUS_PROTO);
    Throwable nestedSre = new Throwable(sre);

    com.google.rpc.Status extractedStatusProto = StatusProto.fromThrowable(sre);
    com.google.rpc.Status extractedStatusProtoFromNestedSre = StatusProto.fromThrowable(nestedSre);

    assertEquals(extractedStatusProto, extractedStatusProtoFromNestedSre);
  }

  @Test
  public void fromThrowableWithNestedStatusException() {
    StatusException se = StatusProto.toStatusException(STATUS_PROTO);
    Throwable nestedSe = new Throwable(se);

    com.google.rpc.Status extractedStatusProto = StatusProto.fromThrowable(se);
    com.google.rpc.Status extractedStatusProtoFromNestedSe = StatusProto.fromThrowable(nestedSe);

    assertEquals(extractedStatusProto, extractedStatusProtoFromNestedSe);
  }

  @Test
  public void fromThrowable_shouldReturnNullIfNoEmbeddedStatus() {
    Throwable nestedSe = new Throwable(new Throwable("no status found"));

    assertNull(StatusProto.fromThrowable(nestedSe));
  }

  private static final Metadata.Key<String> METADATA_KEY =
      Metadata.Key.of("test-metadata", Metadata.ASCII_STRING_MARSHALLER);
  private static final String METADATA_VALUE = "test metadata value";
  private static final com.google.rpc.Status STATUS_PROTO =
      com.google.rpc.Status.newBuilder()
          .setCode(2)
          .setMessage("status message")
          .addDetails(
              com.google.protobuf.Any.pack(
                  com.google.rpc.Status.newBuilder()
                      .setCode(13)
                      .setMessage("nested message")
                      .build()))
          .build();
  private static final com.google.rpc.Status INVALID_STATUS_PROTO =
      com.google.rpc.Status.newBuilder().setCode(-1).build();
}
