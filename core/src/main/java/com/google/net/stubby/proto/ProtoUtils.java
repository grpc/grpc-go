package com.google.net.stubby.proto;

import com.google.net.stubby.Marshaller;
import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.protobuf.GeneratedMessage;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.MessageLite;
import com.google.protobuf.Parser;

import java.io.InputStream;

/**
 * Utility methods for using protobuf with grpc.
 */
public class ProtoUtils {

  public static <T extends MessageLite> Marshaller<T> marshaller(final Parser<T> parser) {
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
          throw Status.INTERNAL.withDescription("Invalid protobuf byte sequence")
            .withCause(ipbe).asRuntimeException();
        }
      }
    };
  }

  /**
   * Produce a metadata key for a generated protobuf type.
   */
  public static <T extends GeneratedMessage> Metadata.Key<T> keyForProto(final T instance) {
    return Metadata.Key.of(
        instance.getDescriptorForType().getFullName() + Metadata.BINARY_HEADER_SUFFIX,
        new Metadata.BinaryMarshaller<T>() {
          @Override
          public byte[] toBytes(T value) {
            return value.toByteArray();
          }

          @Override
          @SuppressWarnings("unchecked")
          public T parseBytes(byte[] serialized) {
            try {
              return (T) instance.getParserForType().parseFrom(serialized);
            } catch (InvalidProtocolBufferException ipbe) {
              throw new IllegalArgumentException(ipbe);
            }
          }
        });
  }

  private ProtoUtils() {
  }
}
