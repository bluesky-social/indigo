package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/ipfs/go-cid"
	"gorm.io/gorm"
)

type Uid uint64

type DbCID struct {
	CID cid.Cid
}

func (dbc *DbCID) Scan(v interface{}) error {
	b, ok := v.([]byte)
	if !ok {
		return fmt.Errorf("dbcids must get bytes!")
	}

	if len(b) == 0 {
		return nil
	}

	c, err := cid.Cast(b)
	if err != nil {
		return err
	}

	dbc.CID = c
	return nil
}

func (dbc DbCID) Value() (driver.Value, error) {
	if !dbc.CID.Defined() {
		return nil, fmt.Errorf("cannot serialize undefined cid to database")
	}
	return dbc.CID.Bytes(), nil
}

func (dbc DbCID) MarshalJSON() ([]byte, error) {
	return json.Marshal(dbc.CID.String())
}

func (dbc *DbCID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	c, err := cid.Decode(s)
	if err != nil {
		return err
	}

	dbc.CID = c
	return nil
}

func (dbc *DbCID) GormDataType() string {
	return "bytes"
}

type PDS struct {
	gorm.Model

	Host       string `gorm:"unique"`
	SSL        bool
	Cursor     int64
	Registered bool
	Blocked    bool

	RateLimit float64

	RepoCount int64
	RepoLimit int64

	HourlyEventLimit int64
	DailyEventLimit  int64
}
