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
  public void fromThrowable_shouldReturnNullIfStatusDetailsKeyIsMissing() {
    Status status = Status.fromCodeValue(0);
    Metadata emptyMetadata = new Metadata();

    assertNull(StatusProto.fromThrowable(status.asRuntimeException(emptyMetadata)));
    assertNull(StatusProto.fromThrowable(status.asException(emptyMetadata)));
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
