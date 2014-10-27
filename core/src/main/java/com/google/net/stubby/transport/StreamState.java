package com.google.net.stubby.transport;

/**
 * The state of a single {@link Stream} within a transport.
 *
 * <p>Client state transitions:<br>
 * OPEN->READ_ONLY->CLOSED (no-error case)<br>
 * OPEN->CLOSED (error) <br>
 * STARTING->CLOSED (Failed creation) <br>
 *
 * <p>Server state transitions:<br>
 * OPEN->WRITE_ONLY->CLOSED (no-error case) <br>
 * OPEN->CLOSED (error case) <br>
 */
public enum StreamState {

  /**
   * The stream is open for write by both endpoints.
   */
  OPEN,

  /**
   * Only the remote endpoint may send data. The local endpoint may only read.
   */
  READ_ONLY,

  /**
   * Only the local endpoint may send data. The remote endpoint may only read.
   */
  WRITE_ONLY,

  /**
   * Neither endpoint may send data.
   */
  CLOSED
}
