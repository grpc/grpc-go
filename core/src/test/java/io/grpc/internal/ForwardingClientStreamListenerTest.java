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

import static org.mockito.ArgumentMatchers.same;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;

import io.grpc.ForwardingTestUtil;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.internal.StreamListener.MessageProducer;
import java.lang.reflect.Method;
import java.util.Collections;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class ForwardingClientStreamListenerTest {
  private ClientStreamListener mock = mock(ClientStreamListener.class);
  private ForwardingClientStreamListener forward = new ForwardingClientStreamListener() {
    @Override
    protected ClientStreamListener delegate() {
      return mock;
    }
  };

  @Test
  public void allMethodsForwarded() throws Exception {
    ForwardingTestUtil.testMethodsForwarded(
        ClientStreamListener.class,
        mock,
        forward,
        Collections.<Method>emptyList());
  }


  @Test
  public void headersReadTest() {
    Metadata headers = new Metadata();
    forward.headersRead(headers);
    verify(mock).headersRead(same(headers));
  }

  @Test
  public void closedTest() {
    Status status = Status.UNKNOWN;
    Metadata trailers = new Metadata();
    forward.closed(status, trailers);
    verify(mock).closed(same(status), same(trailers));
  }

  @Test
  public void messagesAvailableTest() {
    MessageProducer producer = mock(MessageProducer.class);
    forward.messagesAvailable(producer);
    verify(mock).messagesAvailable(same(producer));
  }
}
