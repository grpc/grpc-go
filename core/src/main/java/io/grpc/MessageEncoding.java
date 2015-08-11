package io.grpc;

import static com.google.common.base.Preconditions.checkArgument;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.Collection;
import java.util.Collections;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;
import java.util.zip.GZIPInputStream;
import java.util.zip.GZIPOutputStream;

import javax.annotation.Nullable;

/**
 * Encloses classes related to the compression and decompression of messages.
 */
public final class MessageEncoding {
  private static final ConcurrentMap<String, Decompressor> decompressors =
      initializeDefaultDecompressors();

  /**
   * Special sentinel codec indicating that no compression should be used.  Users should use
   * reference equality to see if compression is disabled.
   */
  public static final Codec NONE = new Codec() {
    @Override
    public InputStream decompress(InputStream is) throws IOException {
      return is;
    }

    @Override
    public String getMessageEncoding() {
      return "identity";
    }

    @Override
    public OutputStream compress(OutputStream os) throws IOException {
      return os;
    }
  };

  /**
   * Represents a message compressor.
   */
  public interface Compressor {
    /**
     * Returns the message encoding that this compressor uses.
     *
     * <p>This can be values such as "gzip", "deflate", "snappy", etc.
     */
    String getMessageEncoding();

    /**
     * Wraps an existing output stream with a compressing output stream.
     * @param os The output stream of uncompressed data
     * @return An output stream that compresses
     */
    OutputStream compress(OutputStream os) throws IOException;
  }

  /**
   * Represents a message decompressor.
   */
  public interface Decompressor {
    /**
     * Returns the message encoding that this compressor uses.
     *
     * <p>This can be values such as "gzip", "deflate", "snappy", etc.
     */
    String getMessageEncoding();

    /**
     * Wraps an existing input stream with a decompressing input stream.
     * @param is The input stream of uncompressed data
     * @return An input stream that decompresses
     */
    InputStream decompress(InputStream is) throws IOException;
  }

  /**
   * Represents an object that can both compress and decompress messages.
   */
  public interface Codec extends Compressor, Decompressor {}

  /**
   * A gzip compressor and decompressor.  In the future this will likely support other
   * compression methods, such as compression level.
   */
  public static final class Gzip implements Codec {
    @Override
    public String getMessageEncoding() {
      return "gzip";
    }

    @Override
    public OutputStream compress(OutputStream os) throws IOException {
      return new GZIPOutputStream(os);
    }

    @Override
    public InputStream decompress(InputStream is) throws IOException {
      return new GZIPInputStream(is);
    }
  }

  /**
   * Registers a decompressor for both decompression and message encoding negotiation.
   * @throws IllegalArgumentException if another compressor by the same name is already registered.
   */
  public static final void registerDecompressor(Decompressor d) {
    Decompressor previousDecompressor = decompressors.putIfAbsent(d.getMessageEncoding(), d);
    checkArgument(previousDecompressor == null,
        "A decompressor was already registered: %s", previousDecompressor);
  }

  /**
   * Provides a list of all message encodings that have decompressors available.
   */
  public static Collection<String> getKnownMessageEncodings() {
    return Collections.unmodifiableSet(decompressors.keySet());
  }

  /**
   * Returns a decompressor for the given message encoding, or {@code null} if none has been
   * registered.
   */
  @Nullable
  public static Decompressor lookupDecompressor(String messageEncoding) {
    return decompressors.get(messageEncoding);
  }

  private static ConcurrentMap<String, Decompressor> initializeDefaultDecompressors() {
    ConcurrentMap<String, Decompressor> defaultDecompressors =
        new ConcurrentHashMap<String, Decompressor>();
    Decompressor gzip = new Gzip();
    defaultDecompressors.put(gzip.getMessageEncoding(), gzip);
    return defaultDecompressors;
  }
}

