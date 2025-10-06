package models

type FilterDid struct {
	Did string `gorm:"primaryKey"`
}

type BufferedEvt struct {
	ID         uint   `gorm:"primaryKey"`
	Did        string `gorm:"not null;index"`
	Collection string `gorm:"not null"`
	Rkey       string `gorm:"not null"`
	Action     string `gorm:"not null"`
	Cid        string `gorm:"type:text"`
	Record     string `gorm:"type:text"`
}
