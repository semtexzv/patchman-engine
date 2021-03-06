package structures

import (
    "time"
)

type HostDAO struct {
	ID              int        `json:"id"       gorm:"not null;primary_key" binding:"required"`
	Request         string     `json:"request"  gorm:"not null"             binding:"required"`
	Checksum        string     `json:"checksum" gorm:"not null"             binding:"required"`
	Updated         time.Time  `json:"updated"  gorm:"-"`
}

// db table name, for gorm
func (HostDAO) TableName() string {
	return "hosts"
}
