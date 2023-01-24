package token

import (
	"encoding/base64"
	"encoding/json"
)

type Token struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

func (t *Token) Encode() (string, error) {
	barr, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	s := base64.StdEncoding.EncodeToString(barr)
	return s, nil
}

func (t *Token) Decode(s string) error {
	barr, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	return json.Unmarshal(barr, t)
}
