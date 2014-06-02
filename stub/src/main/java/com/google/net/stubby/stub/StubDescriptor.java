package com.google.net.stubby.stub;

import com.google.net.stubby.DeferredProtoInputStream;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.MessageLite;

import java.io.InputStream;

/**
 * StubDescriptor used by generated stubs
 */
// TODO(user): Should really be an interface
public class StubDescriptor<I extends MessageLite, O extends MessageLite> {

  private final String name;
  private final O defaultO;

  public StubDescriptor(String name, O defaultO) {
    this.name = name;
    this.defaultO = defaultO;
  }

  public String getName() {
    return name;
  }

  public O parseResponse(InputStream input) {
    try {
      return (O) defaultO.getParserForType().parseFrom(input);
    } catch (InvalidProtocolBufferException ipbe) {
      throw new RuntimeException(ipbe);
    }
  }

  public InputStream streamRequest(I input) {
    return new DeferredProtoInputStream(input);
  }
}
