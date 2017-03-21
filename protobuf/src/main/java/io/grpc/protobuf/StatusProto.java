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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkNotNull;

import io.grpc.ExperimentalApi;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusException;
import io.grpc.StatusRuntimeException;
import io.grpc.protobuf.lite.ProtoLiteUtils;
import javax.annotation.Nullable;

/** Utility methods for working with {@link com.google.rpc.Status}. */
@ExperimentalApi
public final class StatusProto {
  private StatusProto() {}

  private static final Metadata.Key<com.google.rpc.Status> STATUS_DETAILS_KEY =
      Metadata.Key.of(
          "grpc-status-details-bin",
          ProtoLiteUtils.metadataMarshaller(com.google.rpc.Status.getDefaultInstance()));

  /**
   * Convert a {@link com.google.rpc.Status} instance to a {@link StatusRuntimeException}.
   *
   * <p>The returned {@link StatusRuntimeException} will wrap a {@link Status} whose code and
   * description are set from the code and message in {@code statusProto}. {@code statusProto} will
   * be serialized and placed into the metadata of the returned {@link StatusRuntimeException}.
   *
   * @throws IllegalArgumentException if the value of {@code statusProto.getCode()} is not a valid
   *     gRPC status code.
   */
  public static StatusRuntimeException toStatusRuntimeException(com.google.rpc.Status statusProto) {
    return toStatus(statusProto).asRuntimeException(toMetadata(statusProto));
  }

  /**
   * Convert a {@link com.google.rpc.Status} instance to a {@link StatusRuntimeException} with
   * additional metadata.
   *
   * <p>The returned {@link StatusRuntimeException} will wrap a {@link Status} whose code and
   * description are set from the code and message in {@code statusProto}. {@code statusProto} will
   * be serialized and added to {@code metadata}. {@code metadata} will be set as the metadata of
   * the returned {@link StatusRuntimeException}.
   *
   * @throws IllegalArgumentException if the value of {@code statusProto.getCode()} is not a valid
   *     gRPC status code.
   */
  public static StatusRuntimeException toStatusRuntimeException(
      com.google.rpc.Status statusProto, Metadata metadata) {
    return toStatus(statusProto).asRuntimeException(toMetadata(statusProto, metadata));
  }

  /**
   * Convert a {@link com.google.rpc.Status} instance to a {@link StatusException}.
   *
   * <p>The returned {@link StatusException} will wrap a {@link Status} whose code and description
   * are set from the code and message in {@code statusProto}. {@code statusProto} will be
   * serialized and placed into the metadata of the returned {@link StatusException}.
   *
   * @throws IllegalArgumentException if the value of {@code statusProto.getCode()} is not a valid
   *     gRPC status code.
   */
  public static StatusException toStatusException(com.google.rpc.Status statusProto) {
    return toStatus(statusProto).asException(toMetadata(statusProto));
  }

  /**
   * Convert a {@link com.google.rpc.Status} instance to a {@link StatusException} with additional
   * metadata.
   *
   * <p>The returned {@link StatusException} will wrap a {@link Status} whose code and description
   * are set from the code and message in {@code statusProto}. {@code statusProto} will be
   * serialized and added to {@code metadata}. {@code metadata} will be set as the metadata of the
   * returned {@link StatusException}.
   *
   * @throws IllegalArgumentException if the value of {@code statusProto.getCode()} is not a valid
   *     gRPC status code.
   */
  public static StatusException toStatusException(
      com.google.rpc.Status statusProto, Metadata metadata) {
    return toStatus(statusProto).asException(toMetadata(statusProto, metadata));
  }

  private static Status toStatus(com.google.rpc.Status statusProto) {
    Status status = Status.fromCodeValue(statusProto.getCode());
    checkArgument(status.getCode().value() == statusProto.getCode(), "invalid status code");
    return status.withDescription(statusProto.getMessage());
  }

  private static Metadata toMetadata(com.google.rpc.Status statusProto) {
    Metadata metadata = new Metadata();
    metadata.put(STATUS_DETAILS_KEY, statusProto);
    return metadata;
  }

  private static Metadata toMetadata(com.google.rpc.Status statusProto, Metadata metadata) {
    checkNotNull(metadata, "metadata must not be null");
    metadata.discardAll(STATUS_DETAILS_KEY);
    metadata.put(STATUS_DETAILS_KEY, statusProto);
    return metadata;
  }

  /**
   * Extract a {@link com.google.rpc.Status} instance from the causal chain of a {@link Throwable}.
   *
   * @return the extracted {@link com.google.rpc.Status} instance, or {@code null} if none exists.
   * @throws IllegalArgumentException if an embedded {@link com.google.rpc.Status} is found and its
   *     code does not match the gRPC {@link Status} code.
   */
  @Nullable
  public static com.google.rpc.Status fromThrowable(Throwable t) {
    Throwable cause = checkNotNull(t, "t");
    while (cause != null) {
      if (cause instanceof StatusException) {
        StatusException e = (StatusException) cause;
        return toStatusProto(e.getStatus(), e.getTrailers());
      } else if (cause instanceof StatusRuntimeException) {
        StatusRuntimeException e = (StatusRuntimeException) cause;
        return toStatusProto(e.getStatus(), e.getTrailers());
      }
      cause = cause.getCause();
    }
    return null;
  }

  @Nullable
  private static com.google.rpc.Status toStatusProto(Status status, Metadata trailers) {
    if (trailers != null) {
      com.google.rpc.Status statusProto = trailers.get(STATUS_DETAILS_KEY);
      checkArgument(
          status.getCode().value() == statusProto.getCode(),
          "com.google.rpc.Status code must match gRPC status code");
      return statusProto;
    }
    return null;
  }
}
