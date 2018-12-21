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

import static com.google.common.base.Preconditions.checkArgument;
import static com.google.common.base.Preconditions.checkState;
import static com.google.common.base.Verify.verify;

import com.google.common.primitives.Ints;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufAllocator;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;

/** Frame protector that uses the ALTS framing. */
public final class AltsTsiFrameProtector implements TsiFrameProtector {
  private static final int HEADER_LEN_FIELD_BYTES = 4;
  private static final int HEADER_TYPE_FIELD_BYTES = 4;
  private static final int HEADER_BYTES = HEADER_LEN_FIELD_BYTES + HEADER_TYPE_FIELD_BYTES;
  private static final int HEADER_TYPE_DEFAULT = 6;
  // Total frame size including full header and tag.
  private static final int MAX_ALLOWED_FRAME_BYTES = 16 * 1024;
  private static final int LIMIT_MAX_ALLOWED_FRAME_BYTES = 1024 * 1024;

  private final Protector protector;
  private final Unprotector unprotector;

  /** Create a new AltsTsiFrameProtector. */
  public AltsTsiFrameProtector(
      int maxProtectedFrameBytes, ChannelCrypterNetty crypter, ByteBufAllocator alloc) {
    checkArgument(maxProtectedFrameBytes > HEADER_BYTES + crypter.getSuffixLength());
    maxProtectedFrameBytes = Math.min(LIMIT_MAX_ALLOWED_FRAME_BYTES, maxProtectedFrameBytes);
    protector = new Protector(maxProtectedFrameBytes, crypter);
    unprotector = new Unprotector(crypter, alloc);
  }

  static int getHeaderLenFieldBytes() {
    return HEADER_LEN_FIELD_BYTES;
  }

  static int getHeaderTypeFieldBytes() {
    return HEADER_TYPE_FIELD_BYTES;
  }

  public static int getHeaderBytes() {
    return HEADER_BYTES;
  }

  static int getHeaderTypeDefault() {
    return HEADER_TYPE_DEFAULT;
  }

  public static int getMaxAllowedFrameBytes() {
    return MAX_ALLOWED_FRAME_BYTES;
  }

  static int getLimitMaxAllowedFrameBytes() {
    return LIMIT_MAX_ALLOWED_FRAME_BYTES;
  }

  @Override
  public void protectFlush(
      List<ByteBuf> unprotectedBufs, Consumer<ByteBuf> ctxWrite, ByteBufAllocator alloc)
      throws GeneralSecurityException {
    protector.protectFlush(unprotectedBufs, ctxWrite, alloc);
  }

  @Override
  public void unprotect(ByteBuf in, List<Object> out, ByteBufAllocator alloc)
      throws GeneralSecurityException {
    unprotector.unprotect(in, out, alloc);
  }

  @Override
  public void destroy() {
    try {
      unprotector.destroy();
    } finally {
      protector.destroy();
    }
  }

  static final class Protector {
    private final int maxUnprotectedBytesPerFrame;
    private final int suffixBytes;
    private ChannelCrypterNetty crypter;

    Protector(int maxProtectedFrameBytes, ChannelCrypterNetty crypter) {
      this.suffixBytes = crypter.getSuffixLength();
      this.maxUnprotectedBytesPerFrame = maxProtectedFrameBytes - HEADER_BYTES - suffixBytes;
      this.crypter = crypter;
    }

    void destroy() {
      // Shared with Unprotector and destroyed there.
      crypter = null;
    }

    void protectFlush(
        List<ByteBuf> unprotectedBufs, Consumer<ByteBuf> ctxWrite, ByteBufAllocator alloc)
        throws GeneralSecurityException {
      checkState(crypter != null, "Cannot protectFlush after destroy.");
      ByteBuf protectedBuf;
      try {
        protectedBuf = handleUnprotected(unprotectedBufs, alloc);
      } finally {
        for (ByteBuf buf : unprotectedBufs) {
          buf.release();
        }
      }
      if (protectedBuf != null) {
        ctxWrite.accept(protectedBuf);
      }
    }

    private ByteBuf handleUnprotected(List<ByteBuf> unprotectedBufs, ByteBufAllocator alloc)
        throws GeneralSecurityException {
      long unprotectedBytes = 0;
      for (ByteBuf buf : unprotectedBufs) {
        unprotectedBytes += buf.readableBytes();
      }
      // Empty plaintext not allowed since this should be handled as no-op in layer above.
      checkArgument(unprotectedBytes > 0);

      // Compute number of frames and allocate a single buffer for all frames.
      long frameNum = unprotectedBytes / maxUnprotectedBytesPerFrame + 1;
      int lastFrameUnprotectedBytes = (int) (unprotectedBytes % maxUnprotectedBytesPerFrame);
      if (lastFrameUnprotectedBytes == 0) {
        frameNum--;
        lastFrameUnprotectedBytes = maxUnprotectedBytesPerFrame;
      }
      long protectedBytes = frameNum * (HEADER_BYTES + suffixBytes) + unprotectedBytes;

      ByteBuf protectedBuf = alloc.directBuffer(Ints.checkedCast(protectedBytes));
      try {
        int bufferIdx = 0;
        for (int frameIdx = 0; frameIdx < frameNum; ++frameIdx) {
          int unprotectedBytesLeft =
              (frameIdx == frameNum - 1) ? lastFrameUnprotectedBytes : maxUnprotectedBytesPerFrame;
          // Write header (at most LIMIT_MAX_ALLOWED_FRAME_BYTES).
          protectedBuf.writeIntLE(unprotectedBytesLeft + HEADER_TYPE_FIELD_BYTES + suffixBytes);
          protectedBuf.writeIntLE(HEADER_TYPE_DEFAULT);

          // Ownership of the backing buffer remains with protectedBuf.
          ByteBuf frameOut = writeSlice(protectedBuf, unprotectedBytesLeft + suffixBytes);
          List<ByteBuf> framePlain = new ArrayList<>();
          while (unprotectedBytesLeft > 0) {
            // Ownership of the buffer backing in remains with unprotectedBufs.
            ByteBuf in = unprotectedBufs.get(bufferIdx);
            if (in.readableBytes() <= unprotectedBytesLeft) {
              // The complete buffer belongs to this frame.
              framePlain.add(in);
              unprotectedBytesLeft -= in.readableBytes();
              bufferIdx++;
            } else {
              // The remainder of in will be part of the next frame.
              framePlain.add(in.readSlice(unprotectedBytesLeft));
              unprotectedBytesLeft = 0;
            }
          }
          crypter.encrypt(frameOut, framePlain);
          verify(!frameOut.isWritable());
        }
        protectedBuf.readerIndex(0);
        protectedBuf.writerIndex(protectedBuf.capacity());
        return protectedBuf.retain();
      } finally {
        protectedBuf.release();
      }
    }
  }

  static final class Unprotector {
    private final int suffixBytes;
    private final ChannelCrypterNetty crypter;

    private DeframerState state = DeframerState.READ_HEADER;
    private int requiredProtectedBytes;
    private ByteBuf header;
    private ByteBuf firstFrameTag;
    private int unhandledIdx = 0;
    private long unhandledBytes = 0;
    private List<ByteBuf> unhandledBufs = new ArrayList<>(16);

    Unprotector(ChannelCrypterNetty crypter, ByteBufAllocator alloc) {
      this.crypter = crypter;
      this.suffixBytes = crypter.getSuffixLength();
      this.header = alloc.directBuffer(HEADER_BYTES);
      this.firstFrameTag = alloc.directBuffer(suffixBytes);
    }

    private void addUnhandled(ByteBuf in) {
      if (in.isReadable()) {
        ByteBuf buf = in.readRetainedSlice(in.readableBytes());
        unhandledBufs.add(buf);
        unhandledBytes += buf.readableBytes();
      }
    }

    void unprotect(ByteBuf in, List<Object> out, ByteBufAllocator alloc)
        throws GeneralSecurityException {
      checkState(header != null, "Cannot unprotect after destroy.");
      addUnhandled(in);
      decodeFrame(alloc, out);
    }

    @SuppressWarnings("fallthrough")
    private void decodeFrame(ByteBufAllocator alloc, List<Object> out)
        throws GeneralSecurityException {
      switch (state) {
        case READ_HEADER:
          if (unhandledBytes < HEADER_BYTES) {
            return;
          }
          handleHeader();
          // fall through
        case READ_PROTECTED_PAYLOAD:
          if (unhandledBytes < requiredProtectedBytes) {
            return;
          }
          ByteBuf unprotectedBuf;
          try {
            unprotectedBuf = handlePayload(alloc);
          } finally {
            clearState();
          }
          if (unprotectedBuf != null) {
            out.add(unprotectedBuf);
          }
          break;
        default:
          throw new AssertionError("impossible enum value");
      }
    }

    private void handleHeader() {
      while (header.isWritable()) {
        ByteBuf in = unhandledBufs.get(unhandledIdx);
        int headerBytesToRead = Math.min(in.readableBytes(), header.writableBytes());
        header.writeBytes(in, headerBytesToRead);
        unhandledBytes -= headerBytesToRead;
        if (!in.isReadable()) {
          unhandledIdx++;
        }
      }
      requiredProtectedBytes = header.readIntLE() - HEADER_TYPE_FIELD_BYTES;
      checkArgument(
          requiredProtectedBytes >= suffixBytes, "Invalid header field: frame size too small");
      checkArgument(
          requiredProtectedBytes <= LIMIT_MAX_ALLOWED_FRAME_BYTES - HEADER_BYTES,
          "Invalid header field: frame size too large");
      int frameType = header.readIntLE();
      checkArgument(frameType == HEADER_TYPE_DEFAULT, "Invalid header field: frame type");
      state = DeframerState.READ_PROTECTED_PAYLOAD;
    }

    private ByteBuf handlePayload(ByteBufAllocator alloc) throws GeneralSecurityException {
      int requiredCiphertextBytes = requiredProtectedBytes - suffixBytes;
      int firstFrameUnprotectedLen = requiredCiphertextBytes;

      // We get the ciphertexts of the first frame and copy over the tag into a single buffer.
      List<ByteBuf> firstFrameCiphertext = new ArrayList<>();
      while (requiredCiphertextBytes > 0) {
        ByteBuf buf = unhandledBufs.get(unhandledIdx);
        if (buf.readableBytes() <= requiredCiphertextBytes) {
          // We use the whole buffer.
          firstFrameCiphertext.add(buf);
          requiredCiphertextBytes -= buf.readableBytes();
          unhandledIdx++;
        } else {
          firstFrameCiphertext.add(buf.readSlice(requiredCiphertextBytes));
          requiredCiphertextBytes = 0;
        }
      }
      int requiredSuffixBytes = suffixBytes;
      while (true) {
        ByteBuf buf = unhandledBufs.get(unhandledIdx);
        if (buf.readableBytes() <= requiredSuffixBytes) {
          // We use the whole buffer.
          requiredSuffixBytes -= buf.readableBytes();
          firstFrameTag.writeBytes(buf);
          if (requiredSuffixBytes == 0) {
            break;
          }
          unhandledIdx++;
        } else {
          firstFrameTag.writeBytes(buf, requiredSuffixBytes);
          break;
        }
      }
      verify(unhandledIdx == unhandledBufs.size() - 1);
      ByteBuf lastBuf = unhandledBufs.get(unhandledIdx);

      // We get the remaining ciphertexts and tags contained in the last buffer.
      List<ByteBuf> ciphertextsAndTags = new ArrayList<>();
      List<Integer> unprotectedLens = new ArrayList<>();
      long requiredUnprotectedBytesCompleteFrames = firstFrameUnprotectedLen;
      while (lastBuf.readableBytes() >= HEADER_BYTES + suffixBytes) {
        // Read frame size.
        int frameSize = lastBuf.readIntLE();
        int payloadSize = frameSize - HEADER_TYPE_FIELD_BYTES - suffixBytes;
        // Break and undo read if we don't have the complete frame yet.
        if (lastBuf.readableBytes() < frameSize) {
          lastBuf.readerIndex(lastBuf.readerIndex() - HEADER_LEN_FIELD_BYTES);
          break;
        }
        // Check the type header.
        checkArgument(lastBuf.readIntLE() == 6);
        // Create a new frame (except for out buffer).
        ciphertextsAndTags.add(lastBuf.readSlice(payloadSize + suffixBytes));
        // Update sizes for frame.
        requiredUnprotectedBytesCompleteFrames += payloadSize;
        unprotectedLens.add(payloadSize);
      }

      // We leave space for suffixBytes to allow for in-place encryption. This allows for calling
      // doFinal in the JCE implementation which can be optimized better than update and doFinal.
      ByteBuf unprotectedBuf =
          alloc.directBuffer(
              Ints.checkedCast(requiredUnprotectedBytesCompleteFrames + suffixBytes));
      try {

        ByteBuf out = writeSlice(unprotectedBuf, firstFrameUnprotectedLen + suffixBytes);
        crypter.decrypt(out, firstFrameTag, firstFrameCiphertext);
        verify(out.writableBytes() == suffixBytes);
        unprotectedBuf.writerIndex(unprotectedBuf.writerIndex() - suffixBytes);

        for (int frameIdx = 0; frameIdx < ciphertextsAndTags.size(); ++frameIdx) {
          out = writeSlice(unprotectedBuf, unprotectedLens.get(frameIdx) + suffixBytes);
          crypter.decrypt(out, ciphertextsAndTags.get(frameIdx));
          verify(out.writableBytes() == suffixBytes);
          unprotectedBuf.writerIndex(unprotectedBuf.writerIndex() - suffixBytes);
        }
        return unprotectedBuf.retain();
      } finally {
        unprotectedBuf.release();
      }
    }

    private void clearState() {
      int bufsSize = unhandledBufs.size();
      ByteBuf lastBuf = unhandledBufs.get(bufsSize - 1);
      boolean keepLast = lastBuf.isReadable();
      for (int bufIdx = 0; bufIdx < (keepLast ? bufsSize - 1 : bufsSize); ++bufIdx) {
        unhandledBufs.get(bufIdx).release();
      }
      unhandledBufs.clear();
      unhandledBytes = 0;
      unhandledIdx = 0;
      if (keepLast) {
        unhandledBufs.add(lastBuf);
        unhandledBytes = lastBuf.readableBytes();
      }
      state = DeframerState.READ_HEADER;
      requiredProtectedBytes = 0;
      header.clear();
      firstFrameTag.clear();
    }

    void destroy() {
      for (ByteBuf unhandledBuf : unhandledBufs) {
        unhandledBuf.release();
      }
      unhandledBufs.clear();
      if (header != null) {
        header.release();
        header = null;
      }
      if (firstFrameTag != null) {
        firstFrameTag.release();
        firstFrameTag = null;
      }
      crypter.destroy();
    }
  }

  private enum DeframerState {
    READ_HEADER,
    READ_PROTECTED_PAYLOAD
  }

  private static ByteBuf writeSlice(ByteBuf in, int len) {
    checkArgument(len <= in.writableBytes());
    ByteBuf out = in.slice(in.writerIndex(), len);
    in.writerIndex(in.writerIndex() + len);
    return out.writerIndex(0);
  }
}
