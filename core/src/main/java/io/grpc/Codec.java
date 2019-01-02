/*
 * Copyright 2015 The gRPC Authors
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

package io.grpc;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.zip.GZIPInputStream;
import java.util.zip.GZIPOutputStream;

/**
 * Encloses classes related to the compression and decompression of messages.
 *
 */
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/1704")
public interface Codec extends Compressor, Decompressor {
  /**
   * A gzip compressor and decompressor.  In the future this will likely support other
   * compression methods, such as compression level.
   */
  final class Gzip implements Codec {
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
   * The "identity", or "none" codec.  This codec is special in that it can be used to explicitly
   * disable Call compression on a Channel that by default compresses.
   */
  final class Identity implements Codec {
    /**
     * Special sentinel codec indicating that no compression should be used.  Users should use
     * reference equality to see if compression is disabled.
     */
    public static final Codec NONE = new Identity();

    @Override
    public InputStream decompress(InputStream is) {
      return is;
    }

    @Override
    public String getMessageEncoding() {
      return "identity";
    }

    @Override
    public OutputStream compress(OutputStream os) {
      return os;
    }

    private Identity() {}
  }
}
