package relay

import "gorm.io/gorm"

type DomainBan struct {
	gorm.Model
	Domain string `gorm:"unique"`
}
