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

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertSame;
import static org.mockito.ArgumentMatchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.Attributes;
import io.grpc.Compressor;
import io.grpc.Decompressor;
import io.grpc.DecompressorRegistry;
import io.grpc.ForwardingTestUtil;
import io.grpc.Status;
import java.io.IOException;
import java.io.InputStream;
import java.lang.reflect.Method;
import java.util.Collections;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class ForwardingClientStreamTest {
  private ClientStream mock = mock(ClientStream.class);
  private ForwardingClientStream forward = new ForwardingClientStream() {
    @Override
    protected ClientStream delegate() {
      return mock;
    }
  };

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ClientStream.class,
        mock,
        forward,
        Collections.<Method>emptyList());
  }

  @Test
  public void requestTest() {
    forward.request(1234);
    verify(mock).request(1234);
  }

  @Test
  public void writeMessageTest() {
    InputStream is = mock(InputStream.class);
    forward.writeMessage(is);
    verify(mock).writeMessage(same(is));
  }

  @Test
  public void isReadyTest() {
    when(mock.isReady()).thenReturn(true);
    assertEquals(true, forward.isReady());
  }

  @Test
  public void setCompressorTest() {
    Compressor compressor = mock(Compressor.class);
    forward.setCompressor(compressor);
    verify(mock).setCompressor(same(compressor));
  }

  @Test
  public void setMessageCompressionTest() {
    forward.setMessageCompression(true);
    verify(mock).setMessageCompression(true);
  }

  @Test
  public void cancelTest() {
    Status reason = Status.UNKNOWN;
    forward.cancel(reason);
    verify(mock).cancel(same(reason));
  }

  @Test
  public void setAuthorityTest() {
    String authority = "authority";
    forward.setAuthority(authority);
    verify(mock).setAuthority(authority);
  }

  @Test
  public void setFullStreamDecompressionTest() {
    forward.setFullStreamDecompression(true);
    verify(mock).setFullStreamDecompression(true);
  }

  @Test
  public void setDecompressorRegistryTest() {
    DecompressorRegistry decompressor =
        DecompressorRegistry.emptyInstance().with(new Decompressor() {
          @Override
          public String getMessageEncoding() {
            return "some-encoding";
          }

          @Override
          public InputStream decompress(InputStream is) throws IOException {
            return is;
          }
        }, true);
    forward.setDecompressorRegistry(decompressor);
    verify(mock).setDecompressorRegistry(same(decompressor));
  }

  @Test
  public void startTest() {
    ClientStreamListener listener = mock(ClientStreamListener.class);
    forward.start(listener);
    verify(mock).start(same(listener));
  }

  @Test
  public void setMaxInboundMessageSizeTest() {
    int size = 4567;
    forward.setMaxInboundMessageSize(size);
    verify(mock).setMaxInboundMessageSize(size);
  }

  @Test
  public void setMaxOutboundMessageSizeTest() {
    int size = 6789;
    forward.setMaxOutboundMessageSize(size);
    verify(mock).setMaxOutboundMessageSize(size);
  }

  @Test
  public void getAttributesTest() {
    Attributes attr = Attributes.newBuilder().build();
    when(mock.getAttributes()).thenReturn(attr);
    assertSame(attr, forward.getAttributes());
  }
}
