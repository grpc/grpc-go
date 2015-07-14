package io.grpc.transport;

import static org.junit.Assert.fail;
import static org.mockito.Mockito.verify;

import com.google.common.collect.ImmutableMultimap;
import com.google.common.collect.Multimap;

import io.grpc.transport.AbstractStream.Phase;

import org.junit.Before;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.ExpectedException;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;

import java.io.InputStream;

import javax.annotation.Nullable;

@RunWith(JUnit4.class)
public class AbstractStreamTest {
  @Rule public final ExpectedException thrown = ExpectedException.none();

  @Mock private StreamListener streamListener;

  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);
  }

  @Test
  public void onStreamAllocated_shouldNotifyReady() {
    AbstractStream<Object> stream = new AbstractStreamBase<Object>(null);

    stream.onStreamAllocated();

    verify(streamListener).onReady();
  }

  @Test
  public void validPhaseTransitions() {
    AbstractStream<Object> stream = new AbstractStreamBase<Object>(null);
    Multimap<Phase, Phase> validTransitions = ImmutableMultimap.<Phase, Phase>builder()
        .put(Phase.HEADERS, Phase.HEADERS)
        .put(Phase.HEADERS, Phase.MESSAGE)
        .put(Phase.HEADERS, Phase.STATUS)
        .put(Phase.MESSAGE, Phase.MESSAGE)
        .put(Phase.MESSAGE, Phase.STATUS)
        .put(Phase.STATUS, Phase.STATUS)
        .build();

    for (Phase startPhase : Phase.values()) {
      for (Phase endPhase : Phase.values()) {
        if (validTransitions.containsEntry(startPhase, endPhase)) {
          stream.verifyNextPhase(startPhase, endPhase);
        } else {
          try {
            stream.verifyNextPhase(startPhase, endPhase);
            fail();
          } catch (IllegalStateException expected) {
            // continue
          }
        }
      }
    }
  }

  /**
   * Base class for testing.
   */
  private class AbstractStreamBase<IdT> extends AbstractStream<IdT> {
    private AbstractStreamBase(WritableBufferAllocator bufferAllocator) {
      super(bufferAllocator);
    }

    @Override
    public void request(int numMessages) {
      throw new UnsupportedOperationException();
    }

    @Override
    @Nullable
    public IdT id() {
      throw new UnsupportedOperationException();
    }

    @Override
    protected StreamListener listener() {
      return streamListener;
    }

    @Override
    protected void internalSendFrame(WritableBuffer frame, boolean endOfStream, boolean flush) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void receiveMessage(InputStream is) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void inboundDeliveryPaused() {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void remoteEndClosed() {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void returnProcessedBytes(int processedBytes) {
      throw new UnsupportedOperationException();
    }

    @Override
    protected void deframeFailed(Throwable cause) {
      throw new UnsupportedOperationException();
    }
  }
}

