/*
 * Copyright 2015, Google Inc. All rights reserved.
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
@ExperimentalApi("https://github.com/grpc/grpc-java/issues/492")
public interface Codec extends Compressor, Decompressor {
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
   * The "identity", or "none" codec.  This codec is special in that it can be used to explicitly
   * disable Call compression on a Channel that by default compresses.
   */
  public static final class Identity implements Codec {
    /**
     * Special sentinel codec indicating that no compression should be used.  Users should use
     * reference equality to see if compression is disabled.
     */
    public static final Codec NONE = new Identity();

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

    private Identity() {}
  }
}
