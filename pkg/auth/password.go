package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	// pbkdf2Iterations is the number of PBKDF2 iterations. 100,000 is
	// the OWASP recommendation for HMAC-SHA256.
	pbkdf2Iterations = 100_000

	// pbkdf2SaltLen is the salt length in bytes (16 bytes = 128 bits).
	pbkdf2SaltLen = 16

	// pbkdf2KeyLen is the derived key length in bytes (32 bytes = 256 bits).
	pbkdf2KeyLen = 32
)

// HashPassword hashes a plaintext password using PBKDF2-HMAC-SHA256.
// The returned string is self-contained and safe to store in the database:
//
//	pbkdf2$100000$<salt_hex>$<hash_hex>
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	hash := pbkdf2Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen)

	return fmt.Sprintf("pbkdf2$%d$%s$%s",
		pbkdf2Iterations,
		hex.EncodeToString(salt),
		hex.EncodeToString(hash),
	), nil
}

// VerifyPassword checks a plaintext password against a hash produced
// by HashPassword. Returns true if the password matches.
func VerifyPassword(hash, password string) bool {
	parts := strings.SplitN(hash, "$", 4)
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}

	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}

	expected, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}

	derived := pbkdf2Key([]byte(password), salt, iterations, len(expected))
	return hmac.Equal(derived, expected)
}

// pbkdf2Key derives a key from password and salt using PBKDF2 with
// HMAC-SHA256. Implemented per RFC 2898 / RFC 8018 using only Go stdlib.
func pbkdf2Key(password, salt []byte, iterations, keyLen int) []byte {
	// PBKDF2 derives keyLen bytes by concatenating blocks.
	// Each block is: U1 xor U2 xor ... xor Uc
	// where U1 = PRF(password, salt || INT(blockNum))
	//       Ui = PRF(password, U_{i-1})
	numBlocks := (keyLen + sha256.Size - 1) / sha256.Size
	dk := make([]byte, 0, numBlocks*sha256.Size)

	for block := 1; block <= numBlocks; block++ {
		dk = append(dk, pbkdf2Block(password, salt, iterations, block)...)
	}

	return dk[:keyLen]
}

// pbkdf2Block computes one PBKDF2 block.
func pbkdf2Block(password, salt []byte, iterations, blockNum int) []byte {
	// U1 = PRF(password, salt || INT_32_BE(blockNum))
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write([]byte{
		byte(blockNum >> 24),
		byte(blockNum >> 16),
		byte(blockNum >> 8),
		byte(blockNum),
	})
	u := mac.Sum(nil)

	result := make([]byte, len(u))
	copy(result, u)

	// U2..Uc
	for i := 1; i < iterations; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(u[:0])
		for j := range result {
			result[j] ^= u[j]
		}
	}

	return result
}
