package transfer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

func pbkdf2(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	size := prf.Size()
	blocks := (keyLen + size - 1) / size
	var counter [4]byte
	dk := make([]byte, 0, blocks*size)
	u := make([]byte, size)
	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(counter[:], uint32(block))
		prf.Write(counter[:])
		dk = prf.Sum(dk)
		t := dk[len(dk)-size:]
		copy(u, t)
		for i := 2; i <= iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range u {
				t[j] ^= u[j]
			}
		}
	}
	return dk[:keyLen]
}
