package com.google.net.stubby.newtransport.netty;

/**
 * Command sent from the transport to the Netty channel to send response headers to the client.
 */
class SendResponseHeadersCommand {
  private final int streamId;

  SendResponseHeadersCommand(int streamId) {
    this.streamId = streamId;
  }

  int streamId() {
    return streamId;
  }

  @Override
  public boolean equals(Object that) {
    if (that == null || !that.getClass().equals(SendResponseHeadersCommand.class)) {
      return false;
    }
    SendResponseHeadersCommand thatCmd = (SendResponseHeadersCommand) that;
    return thatCmd.streamId == streamId;
  }

  @Override
  public String toString() {
    return getClass().getSimpleName() + "(streamId=" + streamId + ")";
  }

  @Override
  public int hashCode() {
    return streamId;
  }
}
