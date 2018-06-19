/*
 * Copyright 2014 The gRPC Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package io.grpc.protobuf.lite;

import static com.google.common.base.Preconditions.checkNotNull;

import com.google.common.annotations.VisibleForTesting;
import com.google.protobuf.CodedInputStream;
import com.google.protobuf.ExtensionRegistryLite;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.MessageLite;
import com.google.protobuf.Parser;
import io.grpc.ExperimentalApi;
import io.grpc.KnownLength;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.MethodDescriptor.PrototypeMarshaller;
import io.grpc.Status;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.lang.ref.Reference;
import java.lang.ref.WeakReference;

/**
 * Utility methods for using protobuf with grpc.
 */
@ExperimentalApi("Experimental until Lite is stable in protobuf")
public final class ProtoLiteUtils {

  // default visibility to avoid synthetic accessors
  static volatile ExtensionRegistryLite globalRegistry =
      ExtensionRegistryLite.getEmptyRegistry();

  private static final int BUF_SIZE = 8192;

  /**
   * The same value as {@link io.grpc.internal.GrpcUtil#DEFAULT_MAX_MESSAGE_SIZE}.
   */
  @VisibleForTesting
  static final int DEFAULT_MAX_MESSAGE_SIZE = 4 * 1024 * 1024;

  /**
   * Sets the global registry for proto marshalling shared across all servers and clients.
   *
   * <p>Warning:  This API will likely change over time.  It is not possible to have separate
   * registries per Process, Server, Channel, Service, or Method.  This is intentional until there
   * is a more appropriate API to set them.
   *
   * <p>Warning:  Do NOT modify the extension registry after setting it.  It is thread safe to call
   * {@link #setExtensionRegistry}, but not to modify the underlying object.
   *
   * <p>If you need custom parsing behavior for protos, you will need to make your own
   * {@code MethodDescriptor.Marshaller} for the time being.
   *
   * @since 1.0.0
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1787")
  public static void setExtensionRegistry(ExtensionRegistryLite newRegistry) {
    globalRegistry = checkNotNull(newRegistry, "newRegistry");
  }

  /**
   * Creates a {@link Marshaller} for protos of the same type as {@code defaultInstance}.
   *
   * @since 1.0.0
   */
  public static <T extends MessageLite> Marshaller<T> marshaller(T defaultInstance) {
    // TODO(ejona): consider changing return type to PrototypeMarshaller (assuming ABI safe)
    return new MessageMarshaller<T>(defaultInstance);
  }

  /**
   * Produce a metadata marshaller for a protobuf type.
   *
   * @since 1.0.0
   */
  public static <T extends MessageLite> Metadata.BinaryMarshaller<T> metadataMarshaller(
      T defaultInstance) {
    return new MetadataMarshaller<T>(defaultInstance);
  }

  /** Copies the data from input stream to output stream. */
  static long copy(InputStream from, OutputStream to) throws IOException {
    // Copied from guava com.google.common.io.ByteStreams because its API is unstable (beta)
    checkNotNull(from);
    checkNotNull(to);
    byte[] buf = new byte[BUF_SIZE];
    long total = 0;
    while (true) {
      int r = from.read(buf);
      if (r == -1) {
        break;
      }
      to.write(buf, 0, r);
      total += r;
    }
    return total;
  }

  private ProtoLiteUtils() {
  }

  private static final class MessageMarshaller<T extends MessageLite>
      implements PrototypeMarshaller<T> {
    private static final ThreadLocal<Reference<byte[]>> bufs = new ThreadLocal<Reference<byte[]>>();

    private final Parser<T> parser;
    private final T defaultInstance;

    @SuppressWarnings("unchecked")
    MessageMarshaller(T defaultInstance) {
      this.defaultInstance = defaultInstance;
      parser = (Parser<T>) defaultInstance.getParserForType();
    }


    @SuppressWarnings("unchecked")
    @Override
    public Class<T> getMessageClass() {
      // Precisely T since protobuf doesn't let messages extend other messages.
      return (Class<T>) defaultInstance.getClass();
    }

    @Override
    public T getMessagePrototype() {
      return defaultInstance;
    }

    @Override
    public InputStream stream(T value) {
      return new ProtoInputStream(value, parser);
    }

    @Override
    public T parse(InputStream stream) {
      if (stream instanceof ProtoInputStream) {
        ProtoInputStream protoStream = (ProtoInputStream) stream;
        // Optimization for in-memory transport. Returning provided object is safe since protobufs
        // are immutable.
        //
        // However, we can't assume the types match, so we have to verify the parser matches.
        // Today the parser is always the same for a given proto, but that isn't guaranteed. Even
        // if not, using the same MethodDescriptor would ensure the parser matches and permit us
        // to enable this optimization.
        if (protoStream.parser() == parser) {
          try {
            @SuppressWarnings("unchecked")
            T message = (T) ((ProtoInputStream) stream).message();
            return message;
          } catch (IllegalStateException ex) {
            // Stream must have been read from, which is a strange state. Since the point of this
            // optimization is to be transparent, instead of throwing an error we'll continue,
            // even though it seems likely there's a bug.
          }
        }
      }
      CodedInputStream cis = null;
      try {
        if (stream instanceof KnownLength) {
          int size = stream.available();
          if (size > 0 && size <= DEFAULT_MAX_MESSAGE_SIZE) {
            Reference<byte[]> ref;
            // buf should not be used after this method has returned.
            byte[] buf;
            if ((ref = bufs.get()) == null || (buf = ref.get()) == null || buf.length < size) {
              buf = new byte[size];
              bufs.set(new WeakReference<byte[]>(buf));
            }

            int remaining = size;
            while (remaining > 0) {
              int position = size - remaining;
              int count = stream.read(buf, position, remaining);
              if (count == -1) {
                break;
              }
              remaining -= count;
            }

            if (remaining != 0) {
              int position = size - remaining;
              throw new RuntimeException("size inaccurate: " + size + " != " + position);
            }
            cis = CodedInputStream.newInstance(buf, 0, size);
          } else if (size == 0) {
            return defaultInstance;
          }
        }
      } catch (IOException e) {
        throw new RuntimeException(e);
      }
      if (cis == null) {
        cis = CodedInputStream.newInstance(stream);
      }
      // Pre-create the CodedInputStream so that we can remove the size limit restriction
      // when parsing.
      cis.setSizeLimit(Integer.MAX_VALUE);

      try {
        return parseFrom(cis);
      } catch (InvalidProtocolBufferException ipbe) {
        throw Status.INTERNAL.withDescription("Invalid protobuf byte sequence")
            .withCause(ipbe).asRuntimeException();
      }
    }

    private T parseFrom(CodedInputStream stream) throws InvalidProtocolBufferException {
      T message = parser.parseFrom(stream, globalRegistry);
      try {
        stream.checkLastTagWas(0);
        return message;
      } catch (InvalidProtocolBufferException e) {
        e.setUnfinishedMessage(message);
        throw e;
      }
    }
  }

  private static final class MetadataMarshaller<T extends MessageLite>
      implements Metadata.BinaryMarshaller<T> {

    private final T defaultInstance;

    MetadataMarshaller(T defaultInstance) {
      this.defaultInstance = defaultInstance;
    }

    @Override
    public byte[] toBytes(T value) {
      return value.toByteArray();
    }

    @Override
    @SuppressWarnings("unchecked")
    public T parseBytes(byte[] serialized) {
      try {
        return (T) defaultInstance.getParserForType().parseFrom(serialized, globalRegistry);
      } catch (InvalidProtocolBufferException ipbe) {
        throw new IllegalArgumentException(ipbe);
      }
    }
  }
}
