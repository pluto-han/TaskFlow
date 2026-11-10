package user

import "time"

type User struct {
	ID                 string
	Email              string
	PasswordHash       string `json:"-"`
	CredentialsVersion int64  `json:"-"`
	CreatedAt          time.Time
}
