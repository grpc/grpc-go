/*
 * Copyright 2016, Google Inc. All rights reserved.
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

package io.grpc.stub;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import io.grpc.CallOptions;
import io.grpc.Channel;
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
}
