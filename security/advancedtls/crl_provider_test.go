/*
 *
 * Copyright 2023 gRPC authors.
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
 *
 */

package advancedtls

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/security/advancedtls/testdata"
)

func TestStaticCRLProvider(t *testing.T) {
	p := MakeStaticCRLProvider()
	for i := 1; i <= 6; i++ {
		crl := loadCRL(t, testdata.Path(fmt.Sprintf("crl/%d.crl", i)))
		p.AddCRL(crl)
	}
	certs := makeChain(t, testdata.Path("crl/unrevoked.pem"))
	crl, err := p.CRL(certs[0])
	if err != nil {
		t.Fatalf("TODO fetching from provider")
	}
	if crl == nil {
		t.Fatalf("TODO CRL is nil")
	}
}
