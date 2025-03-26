package main

import (
	"errors"
	"strconv"

	"gorm.io/gorm"
)

// RelaySetting is a gorm model
type RelaySetting struct {
	Name  string `gorm:"primarykey"`
	Value string
}

func getRelaySetting(db *gorm.DB, name string) (value string, found bool, err error) {
	var setting RelaySetting
	dbResult := db.First(&setting, "name = ?", name)
	if errors.Is(dbResult.Error, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if dbResult.Error != nil {
		return "", false, dbResult.Error
	}
	return setting.Value, true, nil
}

func setRelaySetting(db *gorm.DB, name string, value string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var setting RelaySetting
		found := tx.First(&setting, "name = ?", name)
		if errors.Is(found.Error, gorm.ErrRecordNotFound) {
			// ok! create it
			setting.Name = name
			setting.Value = value
			return tx.Create(&setting).Error
		} else if found.Error != nil {
			return found.Error
		}
		setting.Value = value
		return tx.Save(&setting).Error
	})
}

func getRelaySettingBool(db *gorm.DB, name string) (value bool, found bool, err error) {
	strval, found, err := getRelaySetting(db, name)
	if err != nil || !found {
		return false, found, err
	}
	value, err = strconv.ParseBool(strval)
	if err != nil {
		return false, false, err
	}
	return value, true, nil
}
func setRelaySettingBool(db *gorm.DB, name string, value bool) error {
	return setRelaySetting(db, name, strconv.FormatBool(value))
}
