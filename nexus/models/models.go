package models

type FilterDid struct {
	Did string `gorm:"primaryKey"`
}

type FilterCollection struct {
	Collection string `gorm:"primaryKey"`
}

type BufferedEvt struct {
	ID  uint                   `gorm:"primaryKey"`
	Did string                 `gorm:"not null;index"`
	Evt map[string]interface{} `gorm:"serializer:json"`
}
