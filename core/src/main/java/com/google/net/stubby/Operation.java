package com.google.net.stubby;

import java.io.InputStream;

import javax.annotation.Nullable;

/**
 * Base interface of operation implementations. Operations move through a phased execution
 * model of
 * HEADERS->PAYLOAD->FOOTERS->CLOSED
 *
 */
public interface Operation {

  public static enum Phase {
    /**
     * Used to communicate key-value pairs that define common context for the operation but
     * that are not strictly part of the interface. Provided prior to delivering any formal
     * parameters
     */
    HEADERS,
    /**
     * A sequence of delimited parameters to the called service
     */
    PAYLOAD,
    /**
     * Used to communicate key-value pairs that define common context for the call but
     * that are not strictly part of the interface. Provided after all formal parameters have
     * been delivered.
     */
    FOOTERS,
    /**
     * Indicates that the operation is closed and will not accept further input.
     */
    CLOSED
  }

  /**
   * Unique id for this operation within the scope of the session.
   * Should not be treated as a UUID
   */
  public int getId();

  /**
   * The current phase of the operation
   */
  public Phase getPhase();

  /**
   * Add a key-value context value.
   * Allowed when phase = HEADERS | FOOTERS.
   * Valid next phases
   * HEADERS -> PAYLOAD_FRAME | FOOTERS | CLOSED
   * FOOTERS -> CLOSED
   * <p>
   * The {@link InputStream} message must be entirely consumed before this call returns.
   * Implementations should not pass references to this stream across thread boundaries without
   * taking a copy.
   * <p>
   * {@code payload.available()} must return the number of remaining bytes to be read.
   *
   * @return this object
   */
  // TODO(user): Context is an incredibly general term. Consider having two signatures
  // addHeader and addTrailer to follow HTTP nomenclature more closely.
  public Operation addContext(String type, InputStream message, Phase nextPhase);

  /**
   * Send a payload to the receiver, indicates that more may follow.
   * Allowed when phase = PAYLOAD_FRAME
   * Valid next phases
   * PAYLOAD_FRAME -> FOOTERS | CLOSED
   * <p>
   * The {@link InputStream} message must be entirely consumed before this call returns.
   * Implementations should not pass references to this stream across thread boundaries without
   * taking a copy.
   * <p>
   * {@code payload.available()} must return the number of remaining bytes to be read.
   *
   * @return this object
   */
  // TODO(user): We need to decide whether we should have nextPhase. It's a bit confusing because
  // even if we specify nextPhase=CLOSED here, we still need to call close() for the actual state
  // transition.
  public Operation addPayload(InputStream payload, Phase nextPhase);

  /**
   * Progress to the CLOSED phase. More than one call to close is allowed as long their
   * {@link com.google.net.stubby.Status#getCode()} agree. If they do not agree implementations
   * should log the details of the newer status but retain the original one.
   * <p>
   * If an error occurs while implementing close the original passed {@link Status} should
   * be retained if its code is not {@link com.google.net.stubby.transport.Transport.Code#OK}
   * otherwise an appropriate {@link Status} should be formed from the error.
   *
   * @return this object
   */
  public Operation close(Status status);

  /**
   * Return the completion {@link Status} of the call or {@code null} if the operation has
   * not yet completed.
   */
  @Nullable
  public Status getStatus();

  /**
   * Store some arbitrary context with this operation
   */
  public <E> E put(Object key, E value);

  /**
   * Retrieve some arbitrary context from this operation
   */
  public <E> E get(Object key);

  /**
   * Remove some arbitrary context from this operation
   */
  public <E> E remove(Object key);
}
