package com.google.net.stubby.newtransport;

/**
 * The state of a single {@link Stream} within a transport.
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
