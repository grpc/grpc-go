package com.google.net.stubby;

import com.google.common.base.Preconditions;
import com.google.common.base.Throwables;
import com.google.net.stubby.transport.Transport;

import javax.annotation.Nullable;
import javax.annotation.concurrent.Immutable;

/**
 * Defines the status of an operation using the canonical error space.
 */
@Immutable
public class Status {

  public static final Status OK = new Status(Transport.Code.OK);

  public static Status fromThrowable(Throwable t) {
    for (Throwable cause : Throwables.getCausalChain(t)) {
      if (cause instanceof OperationException) {
        return ((Status.OperationException) cause).getStatus();
      } else if (cause instanceof  OperationRuntimeException) {
        return ((Status.OperationRuntimeException) cause).getStatus();
      }
    }
    // Couldn't find a cause with a Status
    return new Status(Transport.Code.INTERNAL, t);
  }

  private final Transport.Code code;
  private final String description;
  private final Throwable cause;

  public Status(Transport.Code code) {
    this(code, null, null);
  }

  public Status(Transport.Code code, @Nullable String description) {
    this(code, description, null);
  }

  public Status(Transport.Code code, @Nullable Throwable cause) {
    this(code, null, cause);
  }

  public Status(Transport.Code code, @Nullable String description, @Nullable Throwable cause) {
    this.code = Preconditions.checkNotNull(code);
    this.description = description;
    this.cause = cause;
  }

  public Transport.Code getCode() {
    return code;
  }

  @Nullable
  public String getDescription() {
    return description;
  }

  @Nullable
  public Throwable getCause() {
    return cause;
  }

  public boolean isOk() {
    return OK.getCode() == getCode();
  }

  /**
   * Override this status with another if allowed.
   */
  public Status overrideWith(Status newStatus) {
    if (this.getCode() == Transport.Code.OK || newStatus.code == Transport.Code.OK) {
      return this;
    } else {
      return newStatus;
    }
  }

  public RuntimeException asRuntimeException() {
    return new OperationRuntimeException(this);
  }

  public Exception asException() {
    return new OperationException(this);
  }

  /**
   * Exception thrown by implementations while managing an operation.
   */
  public static class OperationException extends Exception {

    private final Status status;

    public OperationException(Status status) {
      super(status.getDescription(), status.getCause());
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
      super(status.getDescription(), status.getCause());
      this.status = status;
    }

    public Status getStatus() {
      return status;
    }
  }

  @Override
  public String toString() {
    StringBuilder builder = new StringBuilder();
    builder.append("[").append(code);
    if (description != null) {
      builder.append(";").append(description);
    }
    if (cause != null) {
      builder.append(";").append(cause);
    }
    builder.append("]");
    return builder.toString();
  }
}
