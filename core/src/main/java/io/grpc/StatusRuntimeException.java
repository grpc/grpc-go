/*
 * Copyright 2015 The gRPC Authors
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

import javax.annotation.Nullable;

/**
 * {@link Status} in RuntimeException form, for propagating Status information via exceptions.
 *
 * @see StatusException
 */
public class StatusRuntimeException extends RuntimeException {

  private static final long serialVersionUID = 1950934672280720624L;
  private final Status status;
  private final Metadata trailers;

  /**
   * Constructs the exception with both a status.  See also {@link Status#asException()}.
   *
   * @since 1.0.0
   */
  public StatusRuntimeException(Status status) {
    this(status, null);
  }

  /**
   * Constructs the exception with both a status and trailers.  See also
   * {@link Status#asException(Metadata)}.
   *
   * @since 1.0.0
   */
  public StatusRuntimeException(Status status, @Nullable Metadata trailers) {
    super(Status.formatThrowableMessage(status), status.getCause());
    this.status = status;
    this.trailers = trailers;
  }

  /**
   * Returns the status code as a {@link Status} object.
   *
   * @since 1.0.0
   */
  public final Status getStatus() {
    return status;
  }

  /**
   * Returns the received trailers.
   *
   * @since 1.0.0
   */
  public final Metadata getTrailers() {
    return trailers;
  }
}
