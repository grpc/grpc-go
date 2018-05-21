/*
 * Copyright 2018, gRPC Authors All rights reserved.
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

package io.grpc.services;

import com.google.protobuf.MessageLite;
import java.io.BufferedOutputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * The output file goes to the JVM's temp dir with a prefix of BINARY_INFO. The proto messages
 * are written serially using {@link MessageLite#writeDelimitedTo(OutputStream)}.
 */
class TempFileSink implements BinaryLogSink {
  private static final Logger logger = Logger.getLogger(TempFileSink.class.getName());

  private final String outPath;
  private final OutputStream out;
  private boolean closed;

  TempFileSink() throws IOException {
    File outFile = File.createTempFile("BINARY_INFO.", "");
    outPath = outFile.getPath();
    logger.log(Level.INFO, "Writing binary logs to to {0}", outFile.getAbsolutePath());
    out = new BufferedOutputStream(new FileOutputStream(outFile));
  }

  String getPath() {
    return this.outPath;
  }

  @Override
  public synchronized void write(MessageLite message) {
    if (closed) {
      logger.log(Level.FINEST, "Attempt to write after TempFileSink is closed.");
      return;
    }
    try {
      message.writeDelimitedTo(out);
    } catch (IOException e) {
      logger.log(Level.SEVERE, "Caught exception while writing", e);
      closeQuietly();
    }
  }

  @Override
  public synchronized void close() throws IOException {
    if (closed) {
      return;
    }
    closed = true;
    try {
      out.flush();
    } finally {
      out.close();
    }
  }

  private synchronized void closeQuietly() {
    try {
      close();
    } catch (IOException e) {
      logger.log(Level.SEVERE, "Caught exception while closing", e);
    }
  }
}
