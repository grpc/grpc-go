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

package io.grpc.alts.internal;

import static com.google.common.truth.Truth.assertThat;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.fail;

import com.google.protobuf.ByteString;
import io.grpc.stub.StreamObserver;
import java.io.IOException;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AltsHandshakerStub}. */
@RunWith(JUnit4.class)
public class AltsHandshakerStubTest {
  /** Mock status of handshaker service. */
  private static enum Status {
    OK,
    ERROR,
    COMPLETE
  }

  private AltsHandshakerStub stub;
  private MockWriter writer;

  @Before
  public void setUp() {
    writer = new MockWriter();
    stub = new AltsHandshakerStub(writer);
    writer.setReader(stub.getReaderForTest());
  }

  /** Send a message as in_bytes and expect same message as out_frames echo back. */
  private void sendSuccessfulMessage() throws Exception {
    String message = "hello world";
    HandshakerReq.Builder req =
        HandshakerReq.newBuilder()
            .setNext(
                NextHandshakeMessageReq.newBuilder()
                    .setInBytes(ByteString.copyFromUtf8(message))
                    .build());
    HandshakerResp resp = stub.send(req.build());
    assertEquals(resp.getOutFrames().toStringUtf8(), message);
  }

  /** Send a message and expect an IOException on error. */
  private void sendAndExpectError() throws InterruptedException {
    try {
      stub.send(HandshakerReq.newBuilder().build());
      fail("Exception expected");
    } catch (IOException ex) {
      assertThat(ex).hasMessageThat().contains("Received a terminating error");
    }
  }

  /** Send a message and expect an IOException on closing. */
  private void sendAndExpectComplete() throws InterruptedException {
    try {
      stub.send(HandshakerReq.newBuilder().build());
      fail("Exception expected");
    } catch (IOException ex) {
      assertThat(ex).hasMessageThat().contains("Response stream closed");
    }
  }

  /** Send a message and expect an IOException on unexpected message. */
  private void sendAndExpectUnexpectedMessage() throws InterruptedException {
    try {
      stub.send(HandshakerReq.newBuilder().build());
      fail("Exception expected");
    } catch (IOException ex) {
      assertThat(ex).hasMessageThat().contains("Received an unexpected response");
    }
  }

  @Test
  public void sendSuccessfulMessageTest() throws Exception {
    writer.setServiceStatus(Status.OK);
    sendSuccessfulMessage();
    stub.close();
  }

  @Test
  public void getServiceErrorTest() throws InterruptedException {
    writer.setServiceStatus(Status.ERROR);
    sendAndExpectError();
    stub.close();
  }

  @Test
  public void getServiceCompleteTest() throws Exception {
    writer.setServiceStatus(Status.COMPLETE);
    sendAndExpectComplete();
    stub.close();
  }

  @Test
  public void getUnexpectedMessageTest() throws Exception {
    writer.setServiceStatus(Status.OK);
    writer.sendUnexpectedResponse();
    sendAndExpectUnexpectedMessage();
    stub.close();
  }

  @Test
  public void closeEarlyTest() throws InterruptedException {
    stub.close();
    sendAndExpectComplete();
  }

  private static class MockWriter implements StreamObserver<HandshakerReq> {
    private StreamObserver<HandshakerResp> reader;
    private Status status = Status.OK;

    private void setReader(StreamObserver<HandshakerResp> reader) {
      this.reader = reader;
    }

    private void setServiceStatus(Status status) {
      this.status = status;
    }

    /** Send a handshaker response to reader. */
    private void sendUnexpectedResponse() {
      reader.onNext(HandshakerResp.newBuilder().build());
    }

    /** Mock writer onNext. Will respond based on the server status. */
    @Override
    public void onNext(final HandshakerReq req) {
      switch (status) {
        case OK:
          HandshakerResp.Builder resp = HandshakerResp.newBuilder();
          reader.onNext(resp.setOutFrames(req.getNext().getInBytes()).build());
          break;
        case ERROR:
          reader.onError(new RuntimeException());
          break;
        case COMPLETE:
          reader.onCompleted();
          break;
        default:
          return;
      }
    }

    @Override
    public void onError(Throwable t) {}

    /** Mock writer onComplete. */
    @Override
    public void onCompleted() {
      reader.onCompleted();
    }
  }
}
