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

package io.grpc.internal;

import io.grpc.Attributes;
import io.grpc.Compressor;
import io.grpc.Deadline;
import io.grpc.DecompressorRegistry;
import io.grpc.Status;
import java.io.InputStream;
import javax.annotation.Nonnull;

/**
 * An implementation of {@link ClientStream} that silently does nothing for the operations.
 */
public class NoopClientStream implements ClientStream {
  public static final NoopClientStream INSTANCE = new NoopClientStream();

  @Override
  public void setAuthority(String authority) {}

  @Override
  public void start(ClientStreamListener listener) {}

  @Override
  public Attributes getAttributes() {
    return Attributes.EMPTY;
  }

  @Override
  public void request(int numMessages) {}

  @Override
  public void writeMessage(InputStream message) {}

  @Override
  public void flush() {}

  @Override
  public boolean isReady() {
    return false;
  }

  @Override
  public void cancel(Status status) {}

  @Override
  public void halfClose() {}

  @Override
  public void setMessageCompression(boolean enable) {
    // noop
  }

  @Override
  public void setCompressor(Compressor compressor) {}

  @Override
  public void setFullStreamDecompression(boolean fullStreamDecompression) {}

  @Override
  public void setDecompressorRegistry(DecompressorRegistry decompressorRegistry) {}

  @Override
  public void setMaxInboundMessageSize(int maxSize) {}

  @Override
  public void setMaxOutboundMessageSize(int maxSize) {}

  @Override
  public void setDeadline(@Nonnull Deadline deadline) {}
}
