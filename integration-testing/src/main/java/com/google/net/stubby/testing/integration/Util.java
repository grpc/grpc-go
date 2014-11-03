package com.google.net.stubby.testing.integration;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.proto.ProtoUtils;
import com.google.protobuf.MessageLite;

import org.junit.Assert;

import java.io.IOException;
import java.net.ServerSocket;
import java.util.List;

/**
 * Utility methods to support integration testing.
 */
public class Util {

  public static final Metadata.Key<Messages.SimpleContext> METADATA_KEY =
      ProtoUtils.keyForProto(Messages.SimpleContext.getDefaultInstance());

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

  public static void assertEquals(MessageLite expected, MessageLite actual) {
    if (expected == null || actual == null) {
      Assert.assertEquals(expected, actual);
    } else {
      if (!expected.equals(actual)) {
        // This assertEquals should always fail.
        Assert.assertEquals(expected.toString(), actual.toString());
        // But if it doesn't, then this should.
        Assert.assertEquals(expected, actual);
        Assert.fail("Messages not equal, but assertEquals didn't throw");
      }
    }
  }

  public static void assertEquals(List<? extends MessageLite> expected,
      List<? extends MessageLite> actual) {
    if (expected == null || actual == null) {
      Assert.assertEquals(expected, actual);
    } else if (expected.size() != actual.size()) {
      Assert.assertEquals(expected, actual);
    } else {
      for (int i = 0; i < expected.size(); i++) {
        try {
          assertEquals(expected.get(i), actual.get(i));
        } catch (Error e) {
          throw new AssertionError("At index " + i, e);
        }
      }
    }
  }
}
