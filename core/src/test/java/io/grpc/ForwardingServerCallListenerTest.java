/*
 * Copyright 2015, gRPC Authors All rights reserved.
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

import static org.mockito.Mockito.verify;

import io.grpc.ForwardingServerCallListener.SimpleForwardingServerCallListener;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

@RunWith(JUnit4.class)
public class ForwardingServerCallListenerTest {

  @Mock private ServerCall.Listener<Void> serverCallListener;
  private ForwardingServerCallListener<Void> forwarder;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
    forwarder = new SimpleForwardingServerCallListener<Void>(serverCallListener) {};
  }

  @Test
  public void onMessage() {
    forwarder.onMessage(null);

    verify(serverCallListener).onMessage(null);
  }

  @Test
  public void onHalfClose() {
    forwarder.onHalfClose();

    verify(serverCallListener).onHalfClose();
  }

  @Test
  public void onCancel() {
    forwarder.onCancel();

    verify(serverCallListener).onCancel();
  }

  @Test
  public void onComplete() {
    forwarder.onComplete();

    verify(serverCallListener).onComplete();
  }

  @Test
  public void onReady() {
    forwarder.onReady();

    verify(serverCallListener).onReady();
  }
}

