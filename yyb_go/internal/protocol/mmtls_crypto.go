package protocol

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const (
	aesKeyLen = 16
	aesIVLen  = 12
	gcmTagLen = 16
)

var (
	labelExpandedSecret = []byte("expanded secret")
	labelHandshakeKeys  = []byte("handshake key expansion")
	labelAppDataKeys    = []byte("application data key expansion")
	labelEarlyKeys      = []byte("early data key expansion")
	labelClientFinished = []byte("client finished")
	labelServerFinished = []byte("server finished")
	labelPSKAccess      = []byte("PSK_ACCESS")
	labelPSKRefresh     = []byte("PSK_REFRESH")
	ilinkHKDFSalt       = []byte("security hdkf expand")
)

func hkdfExpand(prk, info []byte, length int) []byte {
	out := make([]byte, 0, length)
	t := []byte{}
	for counter := byte(1); len(out) < length; counter++ {
		h := hmac.New(sha256.New, prk)
		h.Write(t)
		h.Write(info)
		h.Write([]byte{counter})
		t = h.Sum(nil)
		out = append(out, t...)
	}
	return out[:length]
}

func hkdfExtract(salt, ikm []byte) []byte {
	h := hmac.New(sha256.New, salt)
	h.Write(ikm)
	return h.Sum(nil)
}

func expandLabel(secret, label, context []byte, length int) []byte {
	info := make([]byte, 0, len(label)+len(context))
	info = append(info, label...)
	info = append(info, context...)
	return hkdfExpand(secret, info, length)
}

func generateECDH() (*ecdh.PrivateKey, []byte, error) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return priv, priv.PublicKey().Bytes(), nil
}

func mmtlsSecret(priv *ecdh.PrivateKey, peerPub []byte) ([]byte, error) {
	peer, err := ecdh.P256().NewPublicKey(peerPub)
	if err != nil {
		return nil, err
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(shared)
	return sum[:], nil
}

func nonce(iv []byte, seq uint64) []byte {
	out := append([]byte(nil), iv...)
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], seq)
	for i := 0; i < 8; i++ {
		out[len(out)-8+i] ^= s[i]
	}
	return out
}

func gcmEncrypt(key, iv []byte, seq uint64, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return g.Seal(nil, nonce(iv, seq), plaintext, aad), nil
}

func gcmDecrypt(key, iv []byte, seq uint64, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return g.Open(nil, nonce(iv, seq), ciphertext, aad)
}

func buildRecordAAD(seq uint64, contentType byte, cipherLen int) []byte {
	out := make([]byte, 13)
	binary.BigEndian.PutUint64(out[0:8], seq)
	out[8] = contentType
	out[9] = 0xf1
	out[10] = 0x03
	binary.BigEndian.PutUint16(out[11:13], uint16(cipherLen))
	return out
}

func encryptRecordPayload(key, iv []byte, seq uint64, contentType byte, plaintext []byte) ([]byte, error) {
	aad := buildRecordAAD(seq, contentType, len(plaintext)+gcmTagLen)
	return gcmEncrypt(key, iv, seq, plaintext, aad)
}

func decryptRecordPayload(key, iv []byte, seq uint64, contentType byte, ct []byte) ([]byte, error) {
	aad := buildRecordAAD(seq, contentType, len(ct))
	return gcmDecrypt(key, iv, seq, ct, aad)
}

type trafficKeys struct {
	ClientKey []byte
	ServerKey []byte
	ClientIV  []byte
	ServerIV  []byte
	Key       []byte
	IV        []byte
}

func deriveMaster(secret, hsHash []byte) []byte {
	return expandLabel(secret, labelExpandedSecret, hsHash, 32)
}

func deriveHandshakeKeys(secret, hsHash []byte) trafficKeys {
	kb := expandLabel(secret, labelHandshakeKeys, hsHash, 56)
	return splitBiKeys(kb)
}

func deriveAppKeys(master, hsHash []byte) trafficKeys {
	kb := expandLabel(master, labelAppDataKeys, hsHash, 56)
	return splitBiKeys(kb)
}

func splitBiKeys(kb []byte) trafficKeys {
	return trafficKeys{
		ClientKey: append([]byte(nil), kb[0:16]...),
		ServerKey: append([]byte(nil), kb[16:32]...),
		ClientIV:  append([]byte(nil), kb[32:44]...),
		ServerIV:  append([]byte(nil), kb[44:56]...),
	}
}

func finishedVerifyData(secret []byte, isClient bool, hsHash []byte) []byte {
	label := labelServerFinished
	if isClient {
		label = labelClientFinished
	}
	key := expandLabel(secret, label, nil, 32)
	h := hmac.New(sha256.New, key)
	h.Write(hsHash)
	return h.Sum(nil)
}

func derivePSK(secret, hCertV []byte, pskType int) []byte {
	label := labelPSKRefresh
	if pskType == 1 {
		label = labelPSKAccess
	}
	return expandLabel(secret, label, hCertV, 32)
}

func derivePSKOneWayKeys(psk, label, hsHash []byte) trafficKeys {
	kb := expandLabel(psk, label, hsHash, 28)
	return trafficKeys{Key: append([]byte(nil), kb[:16]...), IV: append([]byte(nil), kb[16:28]...)}
}

func aesGCMEncryptLayout(key, aad, plaintext []byte) ([]byte, error) {
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ct := g.Seal(nil, iv, plaintext, aad)
	tag := ct[len(ct)-gcmTagLen:]
	body := ct[:len(ct)-gcmTagLen]
	out := make([]byte, 0, len(body)+12+16)
	out = append(out, body...)
	out = append(out, iv...)
	out = append(out, tag...)
	return out, nil
}

func aesGCMDecryptLayout(key, aad, blob []byte) ([]byte, error) {
	if len(blob) < 28 {
		return nil, fmt.Errorf("GCM blob too short")
	}
	ct := blob[:len(blob)-28]
	iv := blob[len(blob)-28 : len(blob)-16]
	tag := blob[len(blob)-16:]
	wire := append(append([]byte(nil), ct...), tag...)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return g.Open(nil, iv, wire, aad)
}
