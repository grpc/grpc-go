package com.google.net.stubby.testing.integration;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.stub.MetadataUtils;

import java.io.IOException;
import java.net.ServerSocket;

/**
 * Utility methods to support integration testing.
 */
public class Util {

  public static final Metadata.Key<Messages.SimpleContext> METADATA_KEY =
      MetadataUtils.keyForProto(Messages.SimpleContext.getDefaultInstance());

  /**
   * Picks an unused port.
   */
  public static int pickUnusedPort() {
    ServerSocket serverSocket = null;
    try {
      serverSocket = new ServerSocket(0);
      int port = serverSocket.getLocalPort();
      serverSocket.close();
      return port;
    } catch (IOException e) {
      throw new RuntimeException(e);
    }
  }
}
