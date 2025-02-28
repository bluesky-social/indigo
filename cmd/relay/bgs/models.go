package bgs

import "gorm.io/gorm"

type DomainBan struct {
	gorm.Model
	Domain string
}
