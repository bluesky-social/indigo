package crypto

import (
	"crypto/elliptic"
	"math/big"
)

var curveN_P256 *big.Int = elliptic.P256().Params().N
var curveHalfOrder_P256 *big.Int = new(big.Int).Rsh(curveN_P256, 1)

// Checks if 'S' value from a P-256 signature is "low-S".
// un-reviewed, un-safe code from: https://github.com/golang/go/issues/54549
func sigSIsLowS_P256(s *big.Int) bool {
	return s.Cmp(curveHalfOrder_P256) != 1
}

// Ensures that 'S' value from a P-256 signature is "low-S" variant.
// un-reviewed, un-safe code from: https://github.com/golang/go/issues/54549
func sigSToLowS_P256(s *big.Int) *big.Int {

	if !sigSIsLowS_P256(s) {
		// Set s to N - s that will be then in the lower part of signature space
		// less or equal to half order
		s.Sub(curveN_P256, s)
		return s
	}
	return s
}
