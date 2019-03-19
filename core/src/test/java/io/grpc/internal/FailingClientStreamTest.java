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

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.ClientStreamListener.RpcProgress;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Unit tests for {@link FailingClientStream}.
 */
@RunWith(JUnit4.class)
public class FailingClientStreamTest {

  @Test
  public void processedRpcProgressPopulatedToListener() {
    ClientStreamListener listener = mock(ClientStreamListener.class);
    Status status = Status.UNAVAILABLE;

    ClientStream stream = new FailingClientStream(status);
    stream.start(listener);
    verify(listener).closed(eq(status), eq(RpcProgress.PROCESSED), any(Metadata.class));
  }

  @Test
  public void droppedRpcProgressPopulatedToListener() {
    ClientStreamListener listener = mock(ClientStreamListener.class);
    Status status = Status.UNAVAILABLE;

    ClientStream stream = new FailingClientStream(status, RpcProgress.DROPPED);
    stream.start(listener);
    verify(listener).closed(eq(status), eq(RpcProgress.DROPPED), any(Metadata.class));
  }
}
