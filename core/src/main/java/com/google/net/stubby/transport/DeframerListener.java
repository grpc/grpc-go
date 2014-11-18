package com.google.net.stubby.transport;

/**
 * A listener of deframing events.
 */
public interface DeframerListener {

  /**
   * Called when the given number of bytes has been read from the input source of the deframer.
   *
   * @param numBytes the number of bytes read from the deframer's input source.
   */
  void bytesRead(int numBytes);
}
