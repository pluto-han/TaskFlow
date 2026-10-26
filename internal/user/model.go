package user

import "time"

type User struct {
	ID           string
	Email        string
	PasswordHash string `json:"-"`
	CreatedAt    time.Time
}
