/*
 * Copyright 2014, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.protobuf.lite;

import static com.google.common.base.Preconditions.checkNotNull;

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
import io.grpc.internal.GrpcUtil;
import java.io.IOException;
import java.io.InputStream;
import java.lang.ref.Reference;
import java.lang.ref.WeakReference;

/**
 * Utility methods for using protobuf with grpc.
 */
@ExperimentalApi("Experimental until Lite is stable in protobuf")
public class ProtoLiteUtils {

  private static volatile ExtensionRegistryLite globalRegistry =
      ExtensionRegistryLite.getEmptyRegistry();

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
   */
  @ExperimentalApi("https://github.com/grpc/grpc-java/issues/1787")
  public static void setExtensionRegistry(ExtensionRegistryLite newRegistry) {
    globalRegistry = checkNotNull(newRegistry, "newRegistry");
  }

  private static final ThreadLocal<Reference<byte[]>> bufs = new ThreadLocal<Reference<byte[]>>() {

    @Override
    protected Reference<byte[]> initialValue() {
      return new WeakReference<byte[]>(new byte[4096]); // Picked at random.
    }
  };

  /** Create a {@code Marshaller} for protos of the same type as {@code defaultInstance}. */
  public static <T extends MessageLite> Marshaller<T> marshaller(final T defaultInstance) {
    @SuppressWarnings("unchecked")
    final Parser<T> parser = (Parser<T>) defaultInstance.getParserForType();
    // TODO(ejona): consider changing return type to PrototypeMarshaller (assuming ABI safe)
    return new PrototypeMarshaller<T>() {
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
            if (size > 0 && size <= GrpcUtil.DEFAULT_MAX_MESSAGE_SIZE) {
              // buf should not be used after this method has returned.
              byte[] buf = bufs.get().get();
              if (buf == null || buf.length < size) {
                buf = new byte[size];
                bufs.set(new WeakReference<byte[]>(buf));
              }
              int chunkSize;
              int position = 0;
              while ((chunkSize = stream.read(buf, position, size - position)) != -1) {
                position += chunkSize;
              }
              if (size != position) {
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
    };
  }

  /**
   * Produce a metadata marshaller for a protobuf type.
   */
  public static <T extends MessageLite> Metadata.BinaryMarshaller<T> metadataMarshaller(
      final T instance) {
    return new Metadata.BinaryMarshaller<T>() {
      @Override
      public byte[] toBytes(T value) {
        return value.toByteArray();
      }

      @Override
      @SuppressWarnings("unchecked")
      public T parseBytes(byte[] serialized) {
        try {
          return (T) instance.getParserForType().parseFrom(serialized, globalRegistry);
        } catch (InvalidProtocolBufferException ipbe) {
          throw new IllegalArgumentException(ipbe);
        }
      }
    };
  }

  private ProtoLiteUtils() {
  }
}
