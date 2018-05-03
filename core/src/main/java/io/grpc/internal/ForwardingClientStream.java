/*
 * Copyright 2018 The gRPC Authors
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

import com.google.common.base.MoreObjects;
import io.grpc.Attributes;
import io.grpc.Compressor;
import io.grpc.Deadline;
import io.grpc.DecompressorRegistry;
import io.grpc.Status;
import java.io.InputStream;

abstract class ForwardingClientStream implements ClientStream {
  protected abstract ClientStream delegate();

  @Override
  public void request(int numMessages) {
    delegate().request(numMessages);
  }

  @Override
  public void writeMessage(InputStream message) {
    delegate().writeMessage(message);
  }

  @Override
  public void flush() {
    delegate().flush();
  }

  @Override
  public boolean isReady() {
    return delegate().isReady();
  }

  @Override
  public void setCompressor(Compressor compressor) {
    delegate().setCompressor(compressor);
  }

  @Override
  public void setMessageCompression(boolean enable) {
    delegate().setMessageCompression(enable);
  }

  @Override
  public void cancel(Status reason) {
    delegate().cancel(reason);
  }

  @Override
  public void halfClose() {
    delegate().halfClose();
  }

  @Override
  public void setAuthority(String authority) {
    delegate().setAuthority(authority);
  }

  @Override
  public void setFullStreamDecompression(boolean fullStreamDecompression) {
    delegate().setFullStreamDecompression(fullStreamDecompression);
  }

  @Override
  public void setDecompressorRegistry(DecompressorRegistry decompressorRegistry) {
    delegate().setDecompressorRegistry(decompressorRegistry);
  }

  @Override
  public void start(ClientStreamListener listener) {
    delegate().start(listener);
  }

  @Override
  public void setMaxInboundMessageSize(int maxSize) {
    delegate().setMaxInboundMessageSize(maxSize);
  }

  @Override
  public void setMaxOutboundMessageSize(int maxSize) {
    delegate().setMaxOutboundMessageSize(maxSize);
  }

  @Override
  public void setDeadline(Deadline deadline) {
    delegate().setDeadline(deadline);
  }

  @Override
  public Attributes getAttributes() {
    return delegate().getAttributes();
  }

  @Override
  public String toString() {
    return MoreObjects.toStringHelper(this).add("delegate", delegate()).toString();
  }
}
