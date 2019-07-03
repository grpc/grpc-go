/*
 *
 * Copyright 2013 M-Lab
 * Copyright 2019 gRPC authors.
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

/*
 * digest.go is based on bobziuchkovski/digest
 * Apache License, Version 2.0
 * Copyright 2013 M-Lab
 * See:
 * https://github.com/bobziuchkovski/digest/blob/master/digest.go
 * https://code.google.com/p/mlab-ns2/gae/ns/digest
 */

package grpc

import (
	"crypto"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	// ErrBadChallenge indicates the received challenged is bad.
	ErrBadChallenge = errors.New("challenge is bad")

	// ErrAlgNotImplemented indicates the algorithm requested by the server is not implemented.
	ErrAlgNotImplemented = errors.New("algorithm not implemented")
)

type digestChallenge struct {
	Realm     string
	Domain    string
	Nonce     string
	Opaque    string
	Stale     string
	Algorithm string
	Qop       string
}

type digestCredentials struct {
	Username   string
	Realm      string
	Nonce      string
	DigestURI  string
	Algorithm  string
	Cnonce     string
	Opaque     string
	MessageQop string
	NonceCount int
	method     string
	password   string
}

func parseChallenge(input string) (*digestChallenge, error) {
	const ws = " \n\r\t"
	const qs = `"`
	s := strings.Trim(input, ws)
	if !strings.HasPrefix(s, "Digest ") {
		return nil, ErrBadChallenge
	}
	s = strings.Trim(s[7:], ws)
	sl := strings.Split(s, ", ")
	c := &digestChallenge{
		Algorithm: "MD5",
	}
	var r []string
	for i := range sl {
		r = strings.SplitN(sl[i], "=", 2)
		switch r[0] {
		case "realm":
			c.Realm = strings.Trim(r[1], qs)
		case "domain":
			c.Domain = strings.Trim(r[1], qs)
		case "nonce":
			c.Nonce = strings.Trim(r[1], qs)
		case "opaque":
			c.Opaque = strings.Trim(r[1], qs)
		case "stale":
			c.Stale = strings.Trim(r[1], qs)
		case "algorithm":
			c.Algorithm = strings.Trim(r[1], qs)
			if _, ok := supportedAlgorithms[c.Algorithm]; !ok {
				return nil, ErrAlgNotImplemented
			}
		case "qop":
			//TODO(gavaletz) should be an array of strings?
			c.Qop = strings.Trim(r[1], qs)
		default:
			return nil, ErrBadChallenge
		}
	}
	return c, nil
}

func newDigestCredentials(req *http.Request, c *digestChallenge, username, password string) *digestCredentials {
	return &digestCredentials{
		Username:   username,
		Realm:      c.Realm,
		Nonce:      c.Nonce,
		DigestURI:  req.URL.Host,
		Algorithm:  c.Algorithm,
		Opaque:     c.Opaque,
		MessageQop: c.Qop, // "auth" must be a single value
		NonceCount: 0,
		method:     req.Method,
		password:   password,
	}
}

var supportedAlgorithms = map[string]crypto.Hash{
	"MD5":         crypto.MD5,
	"SHA-512-256": crypto.SHA512_256,
	"SHA-256":     crypto.SHA256,
}

func h(data string, algo string) string {
	hash := supportedAlgorithms[algo]
	hf := hash.New()
	io.WriteString(hf, data)
	return fmt.Sprintf("%x", hf.Sum(nil))
}

func kd(secret, data, algo string) string {
	return h(fmt.Sprintf("%s:%s", secret, data), algo)
}

func (c *digestCredentials) ha1() string {
	return h(fmt.Sprintf("%s:%s:%s", c.Username, c.Realm, c.password), c.Algorithm)
}

func (c *digestCredentials) ha2() string {
	return h(fmt.Sprintf("%s:%s", c.method, c.DigestURI), c.Algorithm)
}

func (c *digestCredentials) resp(cnonce string) (string, error) {
	c.NonceCount++
	if c.MessageQop == "auth" {
		if cnonce != "" {
			c.Cnonce = cnonce
		} else {
			b := make([]byte, 8)
			io.ReadFull(rand.Reader, b)
			c.Cnonce = fmt.Sprintf("%x", b)[:16]
		}
		return kd(c.ha1(), fmt.Sprintf("%s:%08x:%s:%s:%s",
			c.Nonce, c.NonceCount, c.Cnonce, c.MessageQop, c.ha2()), c.Algorithm), nil
	} else if c.MessageQop == "" {
		return kd(c.ha1(), fmt.Sprintf("%s:%s", c.Nonce, c.ha2()), c.Algorithm), nil
	}
	return "", ErrAlgNotImplemented
}

func (c *digestCredentials) authorize() (string, error) {
	// Note that this is only implemented for MD5, SHA-512-256 and SHA-256.
	// But NOT the *-sess variants.
	// *-sess variants are rarely supported and those that do are a big mess.
	if _, ok := supportedAlgorithms[c.Algorithm]; !ok {
		return "", ErrAlgNotImplemented
	}
	// Note that this is NOT implemented for "qop=auth-int".  Similarly the
	// auth-int server side implementations that do exist are a mess.
	if c.MessageQop != "auth" && c.MessageQop != "" {
		return "", ErrAlgNotImplemented
	}
	resp, err := c.resp("")
	if err != nil {
		return "", ErrAlgNotImplemented
	}
	sl := []string{fmt.Sprintf(`username="%s"`, c.Username)}
	sl = append(sl, fmt.Sprintf(`realm="%s"`, c.Realm))
	sl = append(sl, fmt.Sprintf(`nonce="%s"`, c.Nonce))
	sl = append(sl, fmt.Sprintf(`uri="%s"`, c.DigestURI))
	sl = append(sl, fmt.Sprintf(`response="%s"`, resp))
	if c.Algorithm != "" {
		sl = append(sl, fmt.Sprintf(`algorithm="%s"`, c.Algorithm))
	}
	if c.Opaque != "" {
		sl = append(sl, fmt.Sprintf(`opaque="%s"`, c.Opaque))
	}
	if c.MessageQop != "" {
		sl = append(sl, fmt.Sprintf("qop=%s", c.MessageQop))
		sl = append(sl, fmt.Sprintf("nc=%08x", c.NonceCount))
		sl = append(sl, fmt.Sprintf(`cnonce="%s"`, c.Cnonce))
	}
	return fmt.Sprintf("Digest %s", strings.Join(sl, ", ")), nil
}
