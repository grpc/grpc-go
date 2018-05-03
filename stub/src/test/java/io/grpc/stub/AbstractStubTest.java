/*
 * Copyright 2016 The gRPC Authors
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

package io.grpc.stub;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.Mockito.mock;

import io.grpc.CallOptions;
import io.grpc.Channel;
import java.util.concurrent.Executor;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

@RunWith(JUnit4.class)
public class AbstractStubTest {

  @Mock
  Channel channel;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);
  }

  @Test(expected = NullPointerException.class)
  public void channelMustNotBeNull() {
    new NoopStub(null);
  }

  @Test(expected = NullPointerException.class)
  public void callOptionsMustNotBeNull() {
    new NoopStub(channel, null);
  }

  @Test(expected = NullPointerException.class)
  public void channelMustNotBeNull2() {
    new NoopStub(null, CallOptions.DEFAULT);
  }

  @Test()
  public void withWaitForReady() {
    NoopStub stub = new NoopStub(channel);
    CallOptions callOptions = stub.getCallOptions();
    assertFalse(callOptions.isWaitForReady());

    stub = stub.withWaitForReady();
    callOptions = stub.getCallOptions();
    assertTrue(callOptions.isWaitForReady());
  }

  class NoopStub extends AbstractStub<NoopStub> {

    NoopStub(Channel channel) {
      super(channel);
    }

    NoopStub(Channel channel, CallOptions options) {
      super(channel, options);
    }

    @Override
    protected NoopStub build(Channel channel, CallOptions callOptions) {
      return new NoopStub(channel, callOptions);
    }
  }

  @Test
  public void withExecutor() {
    NoopStub stub = new NoopStub(channel);
    CallOptions callOptions = stub.getCallOptions();

    assertNull(callOptions.getExecutor());

    Executor executor = mock(Executor.class);
    stub = stub.withExecutor(executor);
    callOptions = stub.getCallOptions();

    assertEquals(callOptions.getExecutor(), executor);
  }
}
