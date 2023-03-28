package pds

import (
	"testing"
)

func TestVerifyPassword(t *testing.T) {
	type test struct {
		password string
		hash     string
		want     error
	}
	// first three tests were generated using bluesky-social/atproto/packages/pds/src/db/scrypt.ts to
	// verify the interoperability.
	tests := []test{
		{
			password: "passwd",
			hash:     "30799490042907dd9537fdd326bebdc7:72af83bc9df38d0337ed681f5b0692ffa316ff349417d8dccfd8ec962b2494030e2c16b9c097eff165da5e57f767627a0d53ec7c97710055172df4fe8abc637e",
			want:     nil,
		},
		{
			password: "1s0_al",
			hash:     "f25de2ee9c099b56a80845658f6bc27b:58585a3eea6a9d7cf048a86550b85de2be6cd6ef670d5bdbb99da84e32d2b40e579ba244449313ae314d0e27d9b1fe126f00eb4c129e5d5021edfff272c9d29f",
			want:     nil,
		},
		{
			password: "demo",
			hash:     "326e6f3622c72566f42fea5da065d5a1:2e0f4d4781253b77ec59e93b2d82e67c4d63642294e91dd3811b6b1e853f240d29eb79e30b6d5152b6f4395943d3056e98dde447775c4ac0b4775289732786d7",
			want:     nil,
		},
		{
			password: "pass",
			hash:     "invalidformat",
			want:     ErrInvalidUsernameOrPassword,
		},
		{
			password: "passwd",
			hash:     "326e6f3622c72566f42fea5da065d5a1:2e0f4d4781253b77ec59e93b2d82e67c4d63642294e91dd3811b6b1e853f240d29eb79e30b6d5152b6f4395943d3056e98dde447775c4ac0b4775289732786d7",
			want:     ErrInvalidUsernameOrPassword,
		},
	}

	for _, tc := range tests {
		got := verifyPassword(tc.hash, tc.password)
		if tc.want != got {
			t.Fatalf("expected: %v, got: %v %v", tc.want, got, tc.password)
		}
	}
}

func TestEncodePassword(t *testing.T) {
	password := "password"
	hashed, err := encodePassword(password)
	if err != nil {
		t.Fatal(err)
	}
	err = verifyPassword(hashed, password)
	if err != nil {
		t.Fatal(err)
	}
}
