package main

import (
	"flag"
	"fmt"

	boeJwe "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt/jwe"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
)

func main() {
	var (
		privKeyInput string
		pubKeyInput  string
		privKeyFile  string
		pubKeyFile   string
		payload      string

		keys *boeJwe.KeyInput
		err  error
	)

	flag.StringVar(&privKeyInput, "private-key", "", "JWK file to read key from")
	flag.StringVar(&privKeyFile, "private-key-file", "", "JWK file to read key from")
	flag.StringVar(&pubKeyInput, "public-key", "", "JWK file to read key from")
	flag.StringVar(&pubKeyFile, "public-key-file", "", "JWK file to read key from")
	flag.StringVar(&payload, "payload", "", "Payload to encrypt")
	flag.Parse()

	if privKeyFile != "" {
		keys, err = boeJwe.ParseKeysFromFile(privKeyFile, pubKeyFile)
	} else {
		keys, err = boeJwe.ParseKeys(privKeyInput, pubKeyInput)
	}
	if err != nil {
		fmt.Printf("failed to parse key: %s\n", err)
		return
	}

	encrypted, err := jwe.Encrypt([]byte(payload), jwe.WithKey(jwa.RSA_OAEP(), keys.PublicKey))
	if err != nil {
		fmt.Printf("failed to encrypt payload: %s\n", err)
		return
	}
	fmt.Printf("Encrypted: %s\n\n\n", encrypted)

	decrypted, err := jwe.Decrypt(encrypted, jwe.WithKey(jwa.RSA_OAEP(), keys.PrivateKey))
	if err != nil {
		fmt.Printf("failed to decrypt payload: %s\n", err)
		return
	}
	fmt.Printf("Decrypted: %s\n", decrypted)
}
