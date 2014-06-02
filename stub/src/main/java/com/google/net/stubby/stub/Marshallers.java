package com.google.net.stubby.stub;

import com.google.net.stubby.DeferredProtoInputStream;
import com.google.net.stubby.Status;
import com.google.net.stubby.transport.Transport;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.MessageLite;
import com.google.protobuf.Parser;

import java.io.InputStream;

/**
 * Utility functions for working with Marshallers.
 */
public class Marshallers {

  private Marshallers() {}

  public static <T extends MessageLite> Marshaller<T> forProto(final Parser<T> parser) {
    return new Marshaller<T>() {
      @Override
      public InputStream stream(T value) {
        return new DeferredProtoInputStream(value);
      }

      @Override
      public T parse(InputStream stream) {
        try {
          return parser.parseFrom(stream);
        } catch (InvalidProtocolBufferException ipbe) {
          throw new Status(Transport.Code.INTERNAL, "Invalid protobuf byte sequence", ipbe)
              .asRuntimeException();
        }
      }
    };
  }
}
