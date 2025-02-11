//go:build !scylla

package carstore

import "errors"

func NewScyllaStore(addrs []string, keyspace string) (CarStore, error) {
	return nil, errors.New("scylla compiled out")
}
