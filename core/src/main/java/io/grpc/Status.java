/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc;

import com.google.common.base.Objects;
import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;

import java.util.ArrayList;
import java.util.List;
import java.util.TreeMap;

import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;

/**
 * Defines the status of an operation by providing a standard {@link Code} in conjunction with an
 * optional descriptive message.
 *
 * <p>For clients, every remote call will return a status on completion. In the case of errors this
 * status may be propagated to blocking stubs as a {@link java.lang.RuntimeException} or to
 * a listener as an explicit parameter.
 *
 * <p>Similarly servers can report a status by throwing {@link OperationRuntimeException}
 * or by passing the status to a callback.
 *
 * <p>Utility functions are provided to convert a status to an exception and to extract them
 * back out.
 */
@Immutable
public final class Status {

  /**
   * The set of canonical status codes. If new codes are added over time they must choose
   * a numerical value that does not collide with any previously used value.
   */
  public enum Code {
    /**
     * The operation completed successfully.
     */
    OK(0),

    /**
     * The operation was cancelled (typically by the caller).
     */
    CANCELLED(1),

    /**
     * Unknown error.  An example of where this error may be returned is
     * if a Status value received from another address space belongs to
     * an error-space that is not known in this address space.  Also
     * errors raised by APIs that do not return enough error information
     * may be converted to this error.
     */
    UNKNOWN(2),

    /**
     * Client specified an invalid argument.  Note that this differs
     * from FAILED_PRECONDITION.  INVALID_ARGUMENT indicates arguments
     * that are problematic regardless of the state of the system
     * (e.g., a malformed file name).
     */
    INVALID_ARGUMENT(3),

    /**
     * Deadline expired before operation could complete.  For operations
     * that change the state of the system, this error may be returned
     * even if the operation has completed successfully.  For example, a
     * successful response from a server could have been delayed long
     * enough for the deadline to expire.
     */
    DEADLINE_EXCEEDED(4),

    /**
     * Some requested entity (e.g., file or directory) was not found.
     */
    NOT_FOUND(5),

    /**
     * Some entity that we attempted to create (e.g., file or directory) already exists.
     */
    ALREADY_EXISTS(6),

    /**
     * The caller does not have permission to execute the specified
     * operation.  PERMISSION_DENIED must not be used for rejections
     * caused by exhausting some resource (use RESOURCE_EXHAUSTED
     * instead for those errors).  PERMISSION_DENIED must not be
     * used if the caller cannot be identified (use UNAUTHENTICATED
     * instead for those errors).
     */
    PERMISSION_DENIED(7),

    /**
     * Some resource has been exhausted, perhaps a per-user quota, or
     * perhaps the entire file system is out of space.
     */
    RESOURCE_EXHAUSTED(8),

    /**
     * Operation was rejected because the system is not in a state
     * required for the operation's execution.  For example, directory
     * to be deleted may be non-empty, an rmdir operation is applied to
     * a non-directory, etc.
     *
     * <p> A litmus test that may help a service implementor in deciding
     * between FAILED_PRECONDITION, ABORTED, and UNAVAILABLE:
     * (a) Use UNAVAILABLE if the client can retry just the failing call.
     * (b) Use ABORTED if the client should retry at a higher-level
     * (e.g., restarting a read-modify-write sequence).
     * (c) Use FAILED_PRECONDITION if the client should not retry until
     * the system state has been explicitly fixed.  E.g., if an "rmdir"
     * fails because the directory is non-empty, FAILED_PRECONDITION
     * should be returned since the client should not retry unless
     * they have first fixed up the directory by deleting files from it.
     */
    FAILED_PRECONDITION(9),

    /**
     * The operation was aborted, typically due to a concurrency issue
     * like sequencer check failures, transaction aborts, etc.
     *
     * <p> See litmus test above for deciding between FAILED_PRECONDITION,
     * ABORTED, and UNAVAILABLE.
     */
    ABORTED(10),

    /**
     * Operation was attempted past the valid range.  E.g., seeking or
     * reading past end of file.
     *
     * <p>Unlike INVALID_ARGUMENT, this error indicates a problem that may
     * be fixed if the system state changes. For example, a 32-bit file
     * system will generate INVALID_ARGUMENT if asked to read at an
     * offset that is not in the range [0,2^32-1], but it will generate
     * OUT_OF_RANGE if asked to read from an offset past the current
     * file size.
     *
     * <p>There is a fair bit of overlap between FAILED_PRECONDITION and OUT_OF_RANGE.
     * We recommend using OUT_OF_RANGE (the more specific error) when it applies
     * so that callers who are iterating through
     * a space can easily look for an OUT_OF_RANGE error to detect when they are done.
     */
    OUT_OF_RANGE(11),

    /**
     * Operation is not implemented or not supported/enabled in this service.
     */
    UNIMPLEMENTED(12),

    /**
     * Internal errors.  Means some invariants expected by underlying
     * system has been broken.  If you see one of these errors,
     * something is very broken.
     */
    INTERNAL(13),

    /**
     * The service is currently unavailable.  This is a most likely a
     * transient condition and may be corrected by retrying with
     * a backoff.
     *
     * <p>See litmus test above for deciding between FAILED_PRECONDITION,
     * ABORTED, and UNAVAILABLE.
     */
    UNAVAILABLE(14),

    /**
     * Unrecoverable data loss or corruption.
     */
    DATA_LOSS(15),

    /**
     * The request does not have valid authentication credentials for the
     * operation.
     */
    UNAUTHENTICATED(16);

    private final int value;
    private final String valueAscii;

    private Code(int value) {
      this.value = value;
      this.valueAscii = Integer.toString(value);
    }

    /**
     * The numerical value of the code.
     */
    public int value() {
      return value;
    }

    private Status status() {
      return STATUS_LIST.get(value);
    }

    private String valueAscii() {
      return valueAscii;
    }
  }

  // Create the canonical list of Status instances indexed by their code values.
  private static List<Status> STATUS_LIST;
  static {
    TreeMap<Integer, Status> canonicalizer = new TreeMap<Integer, Status>();
    for (Code code : Code.values()) {
      Status replaced = canonicalizer.put(code.value(), new Status(code));
      if (replaced != null) {
        throw new IllegalStateException("Code value duplication between " +
            replaced.getCode().name() + " & " + code.name());
      }
    }
    STATUS_LIST = new ArrayList<Status>(canonicalizer.values());
  }

  // A pseudo-enum of Status instances mapped 1:1 with values in Code. This simplifies construction
  // patterns for derived implementations of Status.
  public static final Status OK = Code.OK.status();
  public static final Status CANCELLED = Code.CANCELLED.status();
  public static final Status UNKNOWN = Code.UNKNOWN.status();
  public static final Status INVALID_ARGUMENT = Code.INVALID_ARGUMENT.status();
  public static final Status DEADLINE_EXCEEDED = Code.DEADLINE_EXCEEDED.status();
  public static final Status NOT_FOUND = Code.NOT_FOUND.status();
  public static final Status ALREADY_EXISTS = Code.ALREADY_EXISTS.status();
  public static final Status PERMISSION_DENIED = Code.PERMISSION_DENIED.status();
  public static final Status UNAUTHENTICATED = Code.UNAUTHENTICATED.status();
  public static final Status RESOURCE_EXHAUSTED = Code.RESOURCE_EXHAUSTED.status();
  public static final Status FAILED_PRECONDITION =
      Code.FAILED_PRECONDITION.status();
  public static final Status ABORTED = Code.ABORTED.status();
  public static final Status OUT_OF_RANGE = Code.OUT_OF_RANGE.status();
  public static final Status UNIMPLEMENTED = Code.UNIMPLEMENTED.status();
  public static final Status INTERNAL = Code.INTERNAL.status();
  public static final Status UNAVAILABLE = Code.UNAVAILABLE.status();
  public static final Status DATA_LOSS = Code.DATA_LOSS.status();

  /**
   * Return a {@link Status} given a canonical error {@link Code} value.
   */
  public static Status fromCodeValue(int codeValue) {
    if (codeValue < 0 || codeValue > STATUS_LIST.size()) {
      return UNKNOWN.withDescription("Unknown code " + codeValue);
    } else {
      return STATUS_LIST.get(codeValue);
    }
  }

  /**
   * Key to bind status code to trailing metadata.
   */
  public static final Metadata.Key<Status> CODE_KEY
      = Metadata.Key.of("grpc-status", new StatusCodeMarshaller());

  /**
   * Key to bind status message to trailing metadata.
   */
  public static final Metadata.Key<String> MESSAGE_KEY
      = Metadata.Key.of("grpc-message", Metadata.ASCII_STRING_MARSHALLER);

  /**
   * Extract an error {@link Status} from the causal chain of a {@link Throwable}.
   */
  public static Status fromThrowable(Throwable t) {
    for (Throwable cause : Throwables.getCausalChain(t)) {
      if (cause instanceof OperationException) {
        return ((Status.OperationException) cause).getStatus();
      } else if (cause instanceof  OperationRuntimeException) {
        return ((Status.OperationRuntimeException) cause).getStatus();
      }
    }
    // Couldn't find a cause with a Status
    return INTERNAL.withCause(t);
  }

  private final Code code;
  private final String description;
  private final Throwable cause;

  private Status(Code code) {
    this(code, null, null);
  }

  private Status(Code code, @Nullable String description, @Nullable Throwable cause) {
    this.code = Preconditions.checkNotNull(code);
    this.description = description;
    this.cause = cause;
  }

  /**
   * Create a derived instance of {@link Status} with the given cause.
   */
  public Status withCause(Throwable cause) {
    if (Objects.equal(this.cause, cause)) {
      return this;
    }
    return new Status(this.code, this.description, cause);
  }

  /**
   * Create a derived instance of {@link Status} with the given description.
   */
  public Status withDescription(String description) {
    if (Objects.equal(this.description, description)) {
      return this;
    }
    return new Status(this.code, description, this.cause);
  }

  /**
   * Create a derived instance of {@link Status} augmenting the current description with
   * additional detail.
   */
  public Status augmentDescription(String additionalDetail) {
    if (additionalDetail == null) {
      return this;
    } else if (this.description == null) {
      return new Status(this.code, additionalDetail, this.cause);
    } else {
      return new Status(this.code, this.description + "\n" + additionalDetail, this.cause);
    }
  }

  /**
   * The canonical status code.
   */
  public Code getCode() {
    return code;
  }

  /**
   * A description of this status for human consumption.
   */
  @Nullable
  public String getDescription() {
    return description;
  }

  /**
   * The underlying cause of an error.
   */
  @Nullable
  public Throwable getCause() {
    return cause;
  }

  /**
   * Is this status OK, i.e. not an error.
   */
  public boolean isOk() {
    return Code.OK == code;
  }

  /**
   * Convert this {@link Status} to a {@link RuntimeException}. Use {@code #fromThrowable}
   * to recover this {@link Status} instance when the returned exception is in the causal chain.
   */
  public RuntimeException asRuntimeException() {
    return new OperationRuntimeException(this);
  }

  /**
   * Convert this {@link Status} to an {@link Exception}. Use {@code #fromThrowable}
   * to recover this {@link Status} instance when the returned exception is in the causal chain.
   */
  public Exception asException() {
    return new OperationException(this);
  }

  /**
   * Exception thrown by implementations while managing an operation.
   */
  public static class OperationException extends Exception {

    private final Status status;

    public OperationException(Status status) {
      super(status.getCode() + ": " + status.getDescription(), status.getCause());
      this.status = status;
    }

    public Status getStatus() {
      return status;
    }
  }

  /**
   * Runtime exception thrown by implementations while managing an operation.
   */
  public static class OperationRuntimeException extends RuntimeException {

    private final Status status;

    public OperationRuntimeException(Status status) {
      super(status.getCode() + ": " + status.getDescription(), status.getCause());
      this.status = status;
    }

    public Status getStatus() {
      return status;
    }
  }

  // We support Guava 14
  @SuppressWarnings("deprecation")
  @Override
  public String toString() {
    return Objects.toStringHelper(this)
        .add("code", code.name())
        .add("description", description)
        .add("cause", cause)
        .toString();
  }

  private static class StatusCodeMarshaller implements Metadata.AsciiMarshaller<Status> {
    @Override
    public String toAsciiString(Status status) {
      return status.getCode().valueAscii();
    }

    @Override
    public Status parseAsciiString(String serialized) {
      return fromCodeValue(Integer.valueOf(serialized));
    }
  }
}
