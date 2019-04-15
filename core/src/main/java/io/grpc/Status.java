/*
 * Copyright 2014 The gRPC Authors
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

package io.grpc;

import static com.google.common.base.Charsets.US_ASCII;
import static com.google.common.base.Charsets.UTF_8;
import static com.google.common.base.Preconditions.checkNotNull;
import static com.google.common.base.Throwables.getStackTraceAsString;

import com.google.common.base.MoreObjects;
import com.google.common.base.Objects;
import io.grpc.Metadata.TrustedAsciiMarshaller;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.TreeMap;
import javax.annotation.CheckReturnValue;
import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;


/**
 * Defines the status of an operation by providing a standard {@link Code} in conjunction with an
 * optional descriptive message. Instances of {@code Status} are created by starting with the
 * template for the appropriate {@link Status.Code} and supplementing it with additional
 * information: {@code Status.NOT_FOUND.withDescription("Could not find 'important_file.txt'");}
 *
 * <p>For clients, every remote call will return a status on completion. In the case of errors this
 * status may be propagated to blocking stubs as a {@link RuntimeException} or to a listener as an
 * explicit parameter.
 *
 * <p>Similarly servers can report a status by throwing {@link StatusRuntimeException}
 * or by passing the status to a callback.
 *
 * <p>Utility functions are provided to convert a status to an exception and to extract them
 * back out.
 */
@Immutable
@CheckReturnValue
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
     * <p>A litmus test that may help a service implementor in deciding
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
     * <p>See litmus test above for deciding between FAILED_PRECONDITION,
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
     * a backoff. Note that it is not always safe to retry
     * non-idempotent operations.
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
    @SuppressWarnings("ImmutableEnumChecker") // we make sure the byte[] can't be modified
    private final byte[] valueAscii;

    private Code(int value) {
      this.value = value;
      this.valueAscii = Integer.toString(value).getBytes(US_ASCII);
    }

    /**
     * The numerical value of the code.
     */
    public int value() {
      return value;
    }

    /**
     * Returns a {@link Status} object corresponding to this status code.
     */
    public Status toStatus() {
      return STATUS_LIST.get(value);
    }

    private byte[] valueAscii() {
      return valueAscii;
    }
  }

  private static final String TEST_EQUALS_FAILURE_PROPERTY = "io.grpc.Status.failOnEqualsForTest";
  private static final boolean FAIL_ON_EQUALS_FOR_TEST =
      Boolean.parseBoolean(System.getProperty(TEST_EQUALS_FAILURE_PROPERTY, "false"));
  
  // Create the canonical list of Status instances indexed by their code values.
  private static final List<Status> STATUS_LIST = buildStatusList();

  private static List<Status> buildStatusList() {
    TreeMap<Integer, Status> canonicalizer = new TreeMap<>();
    for (Code code : Code.values()) {
      Status replaced = canonicalizer.put(code.value(), new Status(code));
      if (replaced != null) {
        throw new IllegalStateException("Code value duplication between "
            + replaced.getCode().name() + " & " + code.name());
      }
    }
    return Collections.unmodifiableList(new ArrayList<>(canonicalizer.values()));
  }

  // A pseudo-enum of Status instances mapped 1:1 with values in Code. This simplifies construction
  // patterns for derived instances of Status.
  /** The operation completed successfully. */
  public static final Status OK = Code.OK.toStatus();
  /** The operation was cancelled (typically by the caller). */
  public static final Status CANCELLED = Code.CANCELLED.toStatus();
  /** Unknown error. See {@link Code#UNKNOWN}. */
  public static final Status UNKNOWN = Code.UNKNOWN.toStatus();
  /** Client specified an invalid argument. See {@link Code#INVALID_ARGUMENT}. */
  public static final Status INVALID_ARGUMENT = Code.INVALID_ARGUMENT.toStatus();
  /** Deadline expired before operation could complete. See {@link Code#DEADLINE_EXCEEDED}. */
  public static final Status DEADLINE_EXCEEDED = Code.DEADLINE_EXCEEDED.toStatus();
  /** Some requested entity (e.g., file or directory) was not found. */
  public static final Status NOT_FOUND = Code.NOT_FOUND.toStatus();
  /** Some entity that we attempted to create (e.g., file or directory) already exists. */
  public static final Status ALREADY_EXISTS = Code.ALREADY_EXISTS.toStatus();
  /**
   * The caller does not have permission to execute the specified operation. See {@link
   * Code#PERMISSION_DENIED}.
   */
  public static final Status PERMISSION_DENIED = Code.PERMISSION_DENIED.toStatus();
  /** The request does not have valid authentication credentials for the operation. */
  public static final Status UNAUTHENTICATED = Code.UNAUTHENTICATED.toStatus();
  /**
   * Some resource has been exhausted, perhaps a per-user quota, or perhaps the entire file system
   * is out of space.
   */
  public static final Status RESOURCE_EXHAUSTED = Code.RESOURCE_EXHAUSTED.toStatus();
  /**
   * Operation was rejected because the system is not in a state required for the operation's
   * execution. See {@link Code#FAILED_PRECONDITION}.
   */
  public static final Status FAILED_PRECONDITION =
      Code.FAILED_PRECONDITION.toStatus();
  /**
   * The operation was aborted, typically due to a concurrency issue like sequencer check failures,
   * transaction aborts, etc. See {@link Code#ABORTED}.
   */
  public static final Status ABORTED = Code.ABORTED.toStatus();
  /** Operation was attempted past the valid range. See {@link Code#OUT_OF_RANGE}. */
  public static final Status OUT_OF_RANGE = Code.OUT_OF_RANGE.toStatus();
  /** Operation is not implemented or not supported/enabled in this service. */
  public static final Status UNIMPLEMENTED = Code.UNIMPLEMENTED.toStatus();
  /** Internal errors. See {@link Code#INTERNAL}. */
  public static final Status INTERNAL = Code.INTERNAL.toStatus();
  /** The service is currently unavailable. See {@link Code#UNAVAILABLE}. */
  public static final Status UNAVAILABLE = Code.UNAVAILABLE.toStatus();
  /** Unrecoverable data loss or corruption. */
  public static final Status DATA_LOSS = Code.DATA_LOSS.toStatus();

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

  private static Status fromCodeValue(byte[] asciiCodeValue) {
    if (asciiCodeValue.length == 1 && asciiCodeValue[0] == '0') {
      return Status.OK;
    }
    return fromCodeValueSlow(asciiCodeValue);
  }

  @SuppressWarnings("fallthrough")
  private static Status fromCodeValueSlow(byte[] asciiCodeValue) {
    int index = 0;
    int codeValue = 0;
    switch (asciiCodeValue.length) {
      case 2:
        if (asciiCodeValue[index] < '0' || asciiCodeValue[index] > '9') {
          break;
        }
        codeValue += (asciiCodeValue[index++] - '0') * 10;
        // fall through
      case 1:
        if (asciiCodeValue[index] < '0' || asciiCodeValue[index] > '9') {
          break;
        }
        codeValue += asciiCodeValue[index] - '0';
        if (codeValue < STATUS_LIST.size()) {
          return STATUS_LIST.get(codeValue);
        }
        break;
      default:
        break;
    }
    return UNKNOWN.withDescription("Unknown code " + new String(asciiCodeValue, US_ASCII));
  }

  /**
   * Return a {@link Status} given a canonical error {@link Code} object.
   */
  public static Status fromCode(Code code) {
    return code.toStatus();
  }

  /**
   * Key to bind status code to trailing metadata.
   */
  static final Metadata.Key<Status> CODE_KEY
      = Metadata.Key.of("grpc-status", false /* not pseudo */, new StatusCodeMarshaller());

  /**
   * Marshals status messages for ({@link #MESSAGE_KEY}.  gRPC does not use binary coding of
   * status messages by default, which makes sending arbitrary strings difficult.  This marshaller
   * uses ASCII printable characters by default, and percent encodes (e.g. %0A) all non ASCII bytes.
   * This leads to normal text being mostly readable (especially useful for debugging), and special
   * text still being sent.
   *
   * <p>By default, the HTTP spec says that header values must be encoded using a strict subset of
   * ASCII (See RFC 7230 section 3.2.6).  HTTP/2 HPACK allows use of arbitrary binary headers, but
   * we do not use them for interoperating with existing HTTP/1.1 code.  Since the grpc-message
   * is encoded to such a header, it needs to not use forbidden characters.
   *
   * <p>This marshaller works by converting the passed in string into UTF-8, checking to see if
   * each individual byte is an allowable byte, and then either percent encoding or passing it
   * through.  When percent encoding, the byte is converted into hexadecimal notation with a '%'
   * prepended.
   *
   * <p>When unmarshalling, bytes are passed through unless they match the "%XX" pattern.  If they
   * do match, the unmarshaller attempts to convert them back into their original UTF-8 byte
   * sequence.  After the input header bytes are converted into UTF-8 bytes, the new byte array is
   * reinterpretted back as a string.
   */
  private static final TrustedAsciiMarshaller<String> STATUS_MESSAGE_MARSHALLER =
      new StatusMessageMarshaller();

  /**
   * Key to bind status message to trailing metadata.
   */
  static final Metadata.Key<String> MESSAGE_KEY =
      Metadata.Key.of("grpc-message", false /* not pseudo */, STATUS_MESSAGE_MARSHALLER);

  /**
   * Extract an error {@link Status} from the causal chain of a {@link Throwable}.
   * If no status can be found, a status is created with {@link Code#UNKNOWN} as its code and
   * {@code t} as its cause.
   *
   * @return non-{@code null} status
   */
  public static Status fromThrowable(Throwable t) {
    Throwable cause = checkNotNull(t, "t");
    while (cause != null) {
      if (cause instanceof StatusException) {
        return ((StatusException) cause).getStatus();
      } else if (cause instanceof StatusRuntimeException) {
        return ((StatusRuntimeException) cause).getStatus();
      }
      cause = cause.getCause();
    }
    // Couldn't find a cause with a Status
    return UNKNOWN.withCause(t);
  }

  /**
   * Extract an error trailers from the causal chain of a {@link Throwable}.
   *
   * @return the trailers or {@code null} if not found.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4683")
  public static Metadata trailersFromThrowable(Throwable t) {
    Throwable cause = checkNotNull(t, "t");
    while (cause != null) {
      if (cause instanceof StatusException) {
        return ((StatusException) cause).getTrailers();
      } else if (cause instanceof StatusRuntimeException) {
        return ((StatusRuntimeException) cause).getTrailers();
      }
      cause = cause.getCause();
    }
    return null;
  }

  static String formatThrowableMessage(Status status) {
    if (status.description == null) {
      return status.code.toString();
    } else {
      return status.code + ": " + status.description;
    }
  }

  private final Code code;
  private final String description;
  private final Throwable cause;

  private Status(Code code) {
    this(code, null, null);
  }

  private Status(Code code, @Nullable String description, @Nullable Throwable cause) {
    this.code = checkNotNull(code, "code");
    this.description = description;
    this.cause = cause;
  }

  /**
   * Create a derived instance of {@link Status} with the given cause.
   * However, the cause is not transmitted from server to client.
   */
  public Status withCause(Throwable cause) {
    if (Objects.equal(this.cause, cause)) {
      return this;
    }
    return new Status(this.code, this.description, cause);
  }

  /**
   * Create a derived instance of {@link Status} with the given description.  Leading and trailing
   * whitespace may be removed; this may change in the future.
   */
  public Status withDescription(String description) {
    if (Objects.equal(this.description, description)) {
      return this;
    }
    return new Status(this.code, description, this.cause);
  }

  /**
   * Create a derived instance of {@link Status} augmenting the current description with
   * additional detail.  Leading and trailing whitespace may be removed; this may change in the
   * future.
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
   * Note that the cause is not transmitted from server to client.
   */
  @Nullable
  public Throwable getCause() {
    return cause;
  }

  /**
   * Is this status OK, i.e., not an error.
   */
  public boolean isOk() {
    return Code.OK == code;
  }

  /**
   * Convert this {@link Status} to a {@link RuntimeException}. Use {@link #fromThrowable}
   * to recover this {@link Status} instance when the returned exception is in the causal chain.
   */
  public StatusRuntimeException asRuntimeException() {
    return new StatusRuntimeException(this);
  }

  /**
   * Same as {@link #asRuntimeException()} but includes the provided trailers in the returned
   * exception.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4683")
  public StatusRuntimeException asRuntimeException(@Nullable Metadata trailers) {
    return new StatusRuntimeException(this, trailers);
  }

  /**
   * Convert this {@link Status} to an {@link Exception}. Use {@link #fromThrowable}
   * to recover this {@link Status} instance when the returned exception is in the causal chain.
   */
  public StatusException asException() {
    return new StatusException(this);
  }

  /**
   * Same as {@link #asException()} but includes the provided trailers in the returned exception.
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/4683")
  public StatusException asException(@Nullable Metadata trailers) {
    return new StatusException(this, trailers);
  }

  /** A string representation of the status useful for debugging. */
  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this)
        .add("code", code.name())
        .add("description", description)
        .add("cause", cause != null ? getStackTraceAsString(cause) : cause)
        .toString();
  }

  private static final class StatusCodeMarshaller implements TrustedAsciiMarshaller<Status> {
    @Override
    public byte[] toAsciiString(Status status) {
      return status.getCode().valueAscii();
    }

    @Override
    public Status parseAsciiString(byte[] serialized) {
      return fromCodeValue(serialized);
    }
  }

  private static final class StatusMessageMarshaller implements TrustedAsciiMarshaller<String> {

    private static final byte[] HEX =
        {'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'B', 'C', 'D', 'E', 'F'};

    @Override
    public byte[] toAsciiString(String value) {
      byte[] valueBytes = value.getBytes(UTF_8);
      for (int i = 0; i < valueBytes.length; i++) {
        byte b = valueBytes[i];
        // If there are only non escaping characters, skip the slow path.
        if (isEscapingChar(b)) {
          return toAsciiStringSlow(valueBytes, i);
        }
      }
      return valueBytes;
    }

    private static boolean isEscapingChar(byte b) {
      return b < ' ' || b >= '~' || b == '%';
    }

    /**
     * @param valueBytes the UTF-8 bytes
     * @param ri The reader index, pointed at the first byte that needs escaping.
     */
    private static byte[] toAsciiStringSlow(byte[] valueBytes, int ri) {
      byte[] escapedBytes = new byte[ri + (valueBytes.length - ri) * 3];
      // copy over the good bytes
      if (ri != 0) {
        System.arraycopy(valueBytes, 0, escapedBytes, 0, ri);
      }
      int wi = ri;
      for (; ri < valueBytes.length; ri++) {
        byte b = valueBytes[ri];
        // Manually implement URL encoding, per the gRPC spec.
        if (isEscapingChar(b)) {
          escapedBytes[wi] = '%';
          escapedBytes[wi + 1] = HEX[(b >> 4) & 0xF];
          escapedBytes[wi + 2] = HEX[b & 0xF];
          wi += 3;
          continue;
        }
        escapedBytes[wi++] = b;
      }
      byte[] dest = new byte[wi];
      System.arraycopy(escapedBytes, 0, dest, 0, wi);

      return dest;
    }

    @SuppressWarnings("deprecation") // Use fast but deprecated String ctor
    @Override
    public String parseAsciiString(byte[] value) {
      for (int i = 0; i < value.length; i++) {
        byte b = value[i];
        if (b < ' ' || b >= '~' || (b == '%' && i + 2 < value.length)) {
          return parseAsciiStringSlow(value);
        }
      }
      return new String(value, 0);
    }

    private static String parseAsciiStringSlow(byte[] value) {
      ByteBuffer buf = ByteBuffer.allocate(value.length);
      for (int i = 0; i < value.length;) {
        if (value[i] == '%' && i + 2 < value.length) {
          try {
            buf.put((byte)Integer.parseInt(new String(value, i + 1, 2, US_ASCII), 16));
            i += 3;
            continue;
          } catch (NumberFormatException e) {
            // ignore, fall through, just push the bytes.
          }
        }
        buf.put(value[i]);
        i += 1;
      }
      return new String(buf.array(), 0, buf.position(), UTF_8);
    }
  }

  /**
   * Equality on Statuses is not well defined.  Instead, do comparison based on their Code with
   * {@link #getCode}.  The description and cause of the Status are unlikely to be stable, and
   * additional fields may be added to Status in the future.
   */
  @Override
  public boolean equals(Object obj) {
    assert !FAIL_ON_EQUALS_FOR_TEST
        : "Status.equals called; disable this by setting " + TEST_EQUALS_FAILURE_PROPERTY;
    return super.equals(obj);
  }

  /**
   * Hash codes on Statuses are not well defined.
   *
   * @see #equals
   */
  @Override
  public int hashCode() {
    return super.hashCode();
  }
}
