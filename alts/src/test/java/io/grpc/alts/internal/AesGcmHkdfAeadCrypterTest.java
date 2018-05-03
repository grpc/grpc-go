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

import static com.google.common.truth.Truth.assertWithMessage;

import com.google.common.io.BaseEncoding;
import java.nio.ByteBuffer;
import java.security.GeneralSecurityException;
import java.util.Arrays;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for {@link AesGcmHkdfAeadCrypter}. */
@RunWith(JUnit4.class)
public final class AesGcmHkdfAeadCrypterTest {

  private static class TestVector {
    final String comment;
    final byte[] key;
    final byte[] nonce;
    final byte[] aad;
    final byte[] plaintext;
    final byte[] ciphertext;

    TestVector(TestVectorBuilder builder) {
      comment = builder.comment;
      key = builder.key;
      nonce = builder.nonce;
      aad = builder.aad;
      plaintext = builder.plaintext;
      ciphertext = builder.ciphertext;
    }

    static TestVectorBuilder builder() {
      return new TestVectorBuilder();
    }
  }

  private static class TestVectorBuilder {
    String comment;
    byte[] key;
    byte[] nonce;
    byte[] aad;
    byte[] plaintext;
    byte[] ciphertext;

    TestVector build() {
      if (comment == null
          && key == null
          && nonce == null
          && aad == null
          && plaintext == null
          && ciphertext == null) {
        throw new IllegalStateException("All fields must be set before calling build().");
      }
      return new TestVector(this);
    }

    TestVectorBuilder withComment(String comment) {
      this.comment = comment;
      return this;
    }

    TestVectorBuilder withKey(String key) {
      this.key = BaseEncoding.base16().lowerCase().decode(key);
      return this;
    }

    TestVectorBuilder withNonce(String nonce) {
      this.nonce = BaseEncoding.base16().lowerCase().decode(nonce);
      return this;
    }

    TestVectorBuilder withAad(String aad) {
      this.aad = BaseEncoding.base16().lowerCase().decode(aad);
      return this;
    }

    TestVectorBuilder withPlaintext(String plaintext) {
      this.plaintext = BaseEncoding.base16().lowerCase().decode(plaintext);
      return this;
    }

    TestVectorBuilder withCiphertext(String ciphertext) {
      this.ciphertext = BaseEncoding.base16().lowerCase().decode(ciphertext);
      return this;
    }
  }

  @Test
  public void testVectorEncrypt() throws GeneralSecurityException {
    int i = 0;
    for (TestVector testVector : testVectors) {
      int bufferSize = testVector.ciphertext.length;
      byte[] ciphertext = new byte[bufferSize];
      ByteBuffer ciphertextBuffer = ByteBuffer.wrap(ciphertext);

      AesGcmHkdfAeadCrypter aeadCrypter = new AesGcmHkdfAeadCrypter(testVector.key);
      aeadCrypter.encrypt(
          ciphertextBuffer,
          ByteBuffer.wrap(testVector.plaintext),
          ByteBuffer.wrap(testVector.aad),
          testVector.nonce);
      String msg = "Failure for test vector " + i;
      assertWithMessage(msg)
          .that(ciphertextBuffer.remaining())
          .isEqualTo(bufferSize - testVector.ciphertext.length);
      byte[] exactCiphertext = Arrays.copyOf(ciphertext, testVector.ciphertext.length);
      assertWithMessage(msg).that(exactCiphertext).isEqualTo(testVector.ciphertext);
      i++;
    }
  }

  @Test
  public void testVectorDecrypt() throws GeneralSecurityException {
    int i = 0;
    for (TestVector testVector : testVectors) {
      // The plaintext buffer might require space for the tag to decrypt (e.g., for conscrypt).
      int bufferSize = testVector.ciphertext.length;
      byte[] plaintext = new byte[bufferSize];
      ByteBuffer plaintextBuffer = ByteBuffer.wrap(plaintext);

      AesGcmHkdfAeadCrypter aeadCrypter = new AesGcmHkdfAeadCrypter(testVector.key);
      aeadCrypter.decrypt(
          plaintextBuffer,
          ByteBuffer.wrap(testVector.ciphertext),
          ByteBuffer.wrap(testVector.aad),
          testVector.nonce);
      String msg = "Failure for test vector " + i;
      assertWithMessage(msg)
          .that(plaintextBuffer.remaining())
          .isEqualTo(bufferSize - testVector.plaintext.length);
      byte[] exactPlaintext = Arrays.copyOf(plaintext, testVector.plaintext.length);
      assertWithMessage(msg).that(exactPlaintext).isEqualTo(testVector.plaintext);
      i++;
    }
  }

  /*
   * NIST vectors from:
   *  http://csrc.nist.gov/groups/ST/toolkit/BCM/documents/proposedmodes/gcm/gcm-revised-spec.pdf
   *
   *  IEEE vectors from:
   *  http://www.ieee802.org/1/files/public/docs2011/bn-randall-test-vectors-0511-v1.pdf
   * Key expanded by setting
   * expandedKey = (key||(key ^ {0x01, .., 0x01})||key ^ {0x02,..,0x02}))[0:44].
   */
  private static final TestVector[] testVectors =
      new TestVector[] {
        TestVector.builder()
            .withComment("Derived from NIST test vector 1")
            .withKey(
                "0000000000000000000000000000000001010101010101010101010101010101020202020202020202"
                    + "020202")
            .withNonce("000000000000000000000000")
            .withAad("")
            .withPlaintext("")
            .withCiphertext("85e873e002f6ebdc4060954eb8675508")
            .build(),
        TestVector.builder()
            .withComment("Derived from NIST test vector 2")
            .withKey(
                "0000000000000000000000000000000001010101010101010101010101010101020202020202020202"
                    + "020202")
            .withNonce("000000000000000000000000")
            .withAad("")
            .withPlaintext("00000000000000000000000000000000")
            .withCiphertext("51e9a8cb23ca2512c8256afff8e72d681aca19a1148ac115e83df4888cc00d11")
            .build(),
        TestVector.builder()
            .withComment("Derived from NIST test vector 3")
            .withKey(
                "feffe9928665731c6d6a8f9467308308fffee8938764721d6c6b8e9566318209fcfdeb908467711e6f"
                    + "688d96")
            .withNonce("cafebabefacedbaddecaf888")
            .withAad("")
            .withPlaintext(
                "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532f"
                    + "cf0e2449a6b525b16aedf5aa0de657ba637b391aafd255")
            .withCiphertext(
                "1018ed5a1402a86516d6576d70b2ffccca261b94df88b58f53b64dfba435d18b2f6e3b7869f9353d4a"
                    + "c8cf09afb1663daa7b4017e6fc2c177c0c087c0df1162129952213cee1bc6e9c8495dd705e1f"
                    + "3d")
            .build(),
        TestVector.builder()
            .withComment("Derived from NIST test vector 4")
            .withKey(
                "feffe9928665731c6d6a8f9467308308fffee8938764721d6c6b8e9566318209fcfdeb908467711e6f"
                    + "688d96")
            .withNonce("cafebabefacedbaddecaf888")
            .withAad("feedfacedeadbeeffeedfacedeadbeefabaddad2")
            .withPlaintext(
                "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532f"
                    + "cf0e2449a6b525b16aedf5aa0de657ba637b39")
            .withCiphertext(
                "1018ed5a1402a86516d6576d70b2ffccca261b94df88b58f53b64dfba435d18b2f6e3b7869f9353d4a"
                    + "c8cf09afb1663daa7b4017e6fc2c177c0c087c4764565d077e9124001ddb27fc0848c5")
            .build(),
        TestVector.builder()
            .withComment(
                "Derived from adapted NIST test vector 4"
                    + " for KDF counter boundary (flip nonce bit 15)")
            .withKey(
                "feffe9928665731c6d6a8f9467308308fffee8938764721d6c6b8e9566318209fcfdeb908467711e6f"
                    + "688d96")
            .withNonce("ca7ebabefacedbaddecaf888")
            .withAad("feedfacedeadbeeffeedfacedeadbeefabaddad2")
            .withPlaintext(
                "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532f"
                    + "cf0e2449a6b525b16aedf5aa0de657ba637b39")
            .withCiphertext(
                "e650d3c0fb879327f2d03287fa93cd07342b136215adbca00c3bd5099ec41832b1d18e0423ed26bb12"
                    + "c6cd09debb29230a94c0cee15903656f85edb6fc509b1b28216382172ecbcc31e1e9b1")
            .build(),
        TestVector.builder()
            .withComment(
                "Derived from adapted NIST test vector 4"
                    + " for KDF counter boundary (flip nonce bit 16)")
            .withKey(
                "feffe9928665731c6d6a8f9467308308fffee8938764721d6c6b8e9566318209fcfdeb908467711e6f"
                    + "688d96")
            .withNonce("cafebbbefacedbaddecaf888")
            .withAad("feedfacedeadbeeffeedfacedeadbeefabaddad2")
            .withPlaintext(
                "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532f"
                    + "cf0e2449a6b525b16aedf5aa0de657ba637b39")
            .withCiphertext(
                "c0121e6c954d0767f96630c33450999791b2da2ad05c4190169ccad9ac86ff1c721e3d82f2ad22ab46"
                    + "3bab4a0754b7dd68ca4de7ea2531b625eda01f89312b2ab957d5c7f8568dd95fcdcd1f")
            .build(),
        TestVector.builder()
            .withComment(
                "Derived from adapted NIST test vector 4"
                    + " for KDF counter boundary (flip nonce bit 63)")
            .withKey(
                "feffe9928665731c6d6a8f9467308308fffee8938764721d6c6b8e9566318209fcfdeb908467711e6f"
                    + "688d96")
            .withNonce("cafebabefacedb2ddecaf888")
            .withAad("feedfacedeadbeeffeedfacedeadbeefabaddad2")
            .withPlaintext(
                "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532f"
                    + "cf0e2449a6b525b16aedf5aa0de657ba637b39")
            .withCiphertext(
                "8af37ea5684a4d81d4fd817261fd9743099e7e6a025eaacf8e54b124fb5743149e05cb89f4a49467fe"
                    + "2e5e5965f29a19f99416b0016b54585d12553783ba59e9f782e82e097c336bf7989f08")
            .build(),
        TestVector.builder()
            .withComment(
                "Derived from adapted NIST test vector 4"
                    + " for KDF counter boundary (flip nonce bit 64)")
            .withKey(
                "feffe9928665731c6d6a8f9467308308fffee8938764721d6c6b8e9566318209fcfdeb908467711e6f"
                    + "688d96")
            .withNonce("cafebabefacedbaddfcaf888")
            .withAad("feedfacedeadbeeffeedfacedeadbeefabaddad2")
            .withPlaintext(
                "d9313225f88406e5a55909c5aff5269a86a7a9531534f7da2e4c303d8a318a721c3c0c95956809532f"
                    + "cf0e2449a6b525b16aedf5aa0de657ba637b39")
            .withCiphertext(
                "fbd528448d0346bfa878634864d407a35a039de9db2f1feb8e965b3ae9356ce6289441d77f8f0df294"
                    + "891f37ea438b223e3bf2bdc53d4c5a74fb680bb312a8dec6f7252cbcd7f5799750ad78")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.1.1 54-byte auth")
            .withKey(
                "ad7a2bd03eac835a6f620fdcb506b345ac7b2ad13fad825b6e630eddb407b244af7829d23cae81586d"
                    + "600dde")
            .withNonce("12153524c0895e81b2c28465")
            .withAad(
                "d609b1f056637a0d46df998d88e5222ab2c2846512153524c0895e8108000f10111213141516171819"
                    + "1a1b1c1d1e1f202122232425262728292a2b2c2d2e2f30313233340001")
            .withPlaintext("")
            .withCiphertext("3ea0b584f3c85e93f9320ea591699efb")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.1.2 54-byte auth")
            .withKey(
                "e3c08a8f06c6e3ad95a70557b23f75483ce33021a9c72b7025666204c69c0b72e1c2888d04c4e1af97"
                    + "a50755")
            .withNonce("12153524c0895e81b2c28465")
            .withAad(
                "d609b1f056637a0d46df998d88e5222ab2c2846512153524c0895e8108000f10111213141516171819"
                    + "1a1b1c1d1e1f202122232425262728292a2b2c2d2e2f30313233340001")
            .withPlaintext("")
            .withCiphertext("294e028bf1fe6f14c4e8f7305c933eb5")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.2.1 60-byte crypt")
            .withKey(
                "ad7a2bd03eac835a6f620fdcb506b345ac7b2ad13fad825b6e630eddb407b244af7829d23cae81586d"
                    + "600dde")
            .withNonce("12153524c0895e81b2c28465")
            .withAad("d609b1f056637a0d46df998d88e52e00b2c2846512153524c0895e81")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435"
                    + "363738393a0002")
            .withCiphertext(
                "db3d25719c6b0a3ca6145c159d5c6ed9aff9c6e0b79f17019ea923b8665ddf52137ad611f0d1bf417a"
                    + "7ca85e45afe106ff9c7569d335d086ae6c03f00987ccd6")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.2.2 60-byte crypt")
            .withKey(
                "e3c08a8f06c6e3ad95a70557b23f75483ce33021a9c72b7025666204c69c0b72e1c2888d04c4e1af97"
                    + "a50755")
            .withNonce("12153524c0895e81b2c28465")
            .withAad("d609b1f056637a0d46df998d88e52e00b2c2846512153524c0895e81")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435"
                    + "363738393a0002")
            .withCiphertext(
                "1641f28ec13afcc8f7903389787201051644914933e9202bb9d06aa020c2a67ef51dfe7bc00a856c55"
                    + "b8f8133e77f659132502bad63f5713d57d0c11e0f871ed")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.3.1 60-byte auth")
            .withKey(
                "071b113b0ca743fecccf3d051f737382061a103a0da642ffcdce3c041e727283051913390ea541fcce"
                    + "cd3f07")
            .withNonce("f0761e8dcd3d000176d457ed")
            .withAad(
                "e20106d7cd0df0761e8dcd3d88e5400076d457ed08000f101112131415161718191a1b1c1d1e1f2021"
                    + "22232425262728292a2b2c2d2e2f303132333435363738393a0003")
            .withPlaintext("")
            .withCiphertext("58837a10562b0f1f8edbe58ca55811d3")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.3.2 60-byte auth")
            .withKey(
                "691d3ee909d7f54167fd1ca0b5d769081f2bde1aee655fdbab80bd5295ae6be76b1f3ceb0bd5f74365"
                    + "ff1ea2")
            .withNonce("f0761e8dcd3d000176d457ed")
            .withAad(
                "e20106d7cd0df0761e8dcd3d88e5400076d457ed08000f101112131415161718191a1b1c1d1e1f2021"
                    + "22232425262728292a2b2c2d2e2f303132333435363738393a0003")
            .withPlaintext("")
            .withCiphertext("c2722ff6ca29a257718a529d1f0c6a3b")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.4.1 54-byte crypt")
            .withKey(
                "071b113b0ca743fecccf3d051f737382061a103a0da642ffcdce3c041e727283051913390ea541fcce"
                    + "cd3f07")
            .withNonce("f0761e8dcd3d000176d457ed")
            .withAad("e20106d7cd0df0761e8dcd3d88e54c2a76d457ed")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333400"
                    + "04")
            .withCiphertext(
                "fd96b715b93a13346af51e8acdf792cdc7b2686f8574c70e6b0cbf16291ded427ad73fec48cd298e05"
                    + "28a1f4c644a949fc31dc9279706ddba33f")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.4.2 54-byte crypt")
            .withKey(
                "691d3ee909d7f54167fd1ca0b5d769081f2bde1aee655fdbab80bd5295ae6be76b1f3ceb0bd5f74365"
                    + "ff1ea2")
            .withNonce("f0761e8dcd3d000176d457ed")
            .withAad("e20106d7cd0df0761e8dcd3d88e54c2a76d457ed")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333400"
                    + "04")
            .withCiphertext(
                "b68f6300c2e9ae833bdc070e24021a3477118e78ccf84e11a485d861476c300f175353d5cdf92008a4"
                    + "f878e6cc3577768085c50a0e98fda6cbb8")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.5.1 65-byte auth")
            .withKey(
                "013fe00b5f11be7f866d0cbbc55a7a90003ee10a5e10bf7e876c0dbac45b7b91033de2095d13bc7d84"
                    + "6f0eb9")
            .withNonce("7cfde9f9e33724c68932d612")
            .withAad(
                "84c5d513d2aaf6e5bbd2727788e523008932d6127cfde9f9e33724c608000f10111213141516171819"
                    + "1a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"
                    + "0005")
            .withPlaintext("")
            .withCiphertext("cca20eecda6283f09bb3543dd99edb9b")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.5.2 65-byte auth")
            .withKey(
                "83c093b58de7ffe1c0da926ac43fb3609ac1c80fee1b624497ef942e2f79a82381c291b78fe5fde3c2"
                    + "d89068")
            .withNonce("7cfde9f9e33724c68932d612")
            .withAad(
                "84c5d513d2aaf6e5bbd2727788e523008932d6127cfde9f9e33724c608000f10111213141516171819"
                    + "1a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f"
                    + "0005")
            .withPlaintext("")
            .withCiphertext("b232cc1da5117bf15003734fa599d271")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE  2.6.1 61-byte crypt")
            .withKey(
                "013fe00b5f11be7f866d0cbbc55a7a90003ee10a5e10bf7e876c0dbac45b7b91033de2095d13bc7d84"
                    + "6f0eb9")
            .withNonce("7cfde9f9e33724c68932d612")
            .withAad("84c5d513d2aaf6e5bbd2727788e52f008932d6127cfde9f9e33724c6")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435"
                    + "363738393a3b0006")
            .withCiphertext(
                "ff1910d35ad7e5657890c7c560146fd038707f204b66edbc3d161f8ace244b985921023c436e3a1c35"
                    + "32ecd5d09a056d70be583f0d10829d9387d07d33d872e490")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.6.2 61-byte crypt")
            .withKey(
                "83c093b58de7ffe1c0da926ac43fb3609ac1c80fee1b624497ef942e2f79a82381c291b78fe5fde3c2"
                    + "d89068")
            .withNonce("7cfde9f9e33724c68932d612")
            .withAad("84c5d513d2aaf6e5bbd2727788e52f008932d6127cfde9f9e33724c6")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435"
                    + "363738393a3b0006")
            .withCiphertext(
                "0db4cf956b5f97eca4eab82a6955307f9ae02a32dd7d93f83d66ad04e1cfdc5182ad12abdea5bbb619"
                    + "a1bd5fb9a573590fba908e9c7a46c1f7ba0905d1b55ffda4")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.7.1 79-byte crypt")
            .withKey(
                "88ee087fd95da9fbf6725aa9d757b0cd89ef097ed85ca8faf7735ba8d656b1cc8aec0a7ddb5fabf9f4"
                    + "7058ab")
            .withNonce("7ae8e2ca4ec500012e58495c")
            .withAad(
                "68f2e77696ce7ae8e2ca4ec588e541002e58495c08000f101112131415161718191a1b1c1d1e1f2021"
                    + "22232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f4041424344454647"
                    + "48494a4b4c4d0007")
            .withPlaintext("")
            .withCiphertext("813f0e630f96fb2d030f58d83f5cdfd0")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.7.2 79-byte crypt")
            .withKey(
                "4c973dbc7364621674f8b5b89e5c15511fced9216490fb1c1a2caa0ffe0407e54e953fbe7166601476"
                    + "fab7ba")
            .withNonce("7ae8e2ca4ec500012e58495c")
            .withAad(
                "68f2e77696ce7ae8e2ca4ec588e541002e58495c08000f101112131415161718191a1b1c1d1e1f2021"
                    + "22232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f4041424344454647"
                    + "48494a4b4c4d0007")
            .withPlaintext("")
            .withCiphertext("77e5a44c21eb07188aacbd74d1980e97")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.8.1 61-byte crypt")
            .withKey(
                "88ee087fd95da9fbf6725aa9d757b0cd89ef097ed85ca8faf7735ba8d656b1cc8aec0a7ddb5fabf9f4"
                    + "7058ab")
            .withNonce("7ae8e2ca4ec500012e58495c")
            .withAad("68f2e77696ce7ae8e2ca4ec588e54d002e58495c")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435"
                    + "363738393a3b3c3d3e3f404142434445464748490008")
            .withCiphertext(
                "958ec3f6d60afeda99efd888f175e5fcd4c87b9bcc5c2f5426253a8b506296c8c43309ab2adb593946"
                    + "2541d95e80811e04e706b1498f2c407c7fb234f8cc01a647550ee6b557b35a7e3945381821"
                    + "f4")
            .build(),
        TestVector.builder()
            .withComment("Derived from IEEE 2.8.2 61-byte crypt")
            .withKey(
                "4c973dbc7364621674f8b5b89e5c15511fced9216490fb1c1a2caa0ffe0407e54e953fbe7166601476"
                    + "fab7ba")
            .withNonce("7ae8e2ca4ec500012e58495c")
            .withAad("68f2e77696ce7ae8e2ca4ec588e54d002e58495c")
            .withPlaintext(
                "08000f101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f303132333435"
                    + "363738393a3b3c3d3e3f404142434445464748490008")
            .withCiphertext(
                "b44d072011cd36d272a9b7a98db9aa90cbc5c67b93ddce67c854503214e2e896ec7e9db649ed4bcf6f"
                    + "850aac0223d0cf92c83db80795c3a17ecc1248bb00591712b1ae71e268164196252162810b"
                    + "00")
            .build()
      };
}
