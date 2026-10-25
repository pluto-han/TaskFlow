package task

import "errors"

var ErrTaskNotFound = errors.New("task not found")

type Repository interface {
	FindByID(id string) (*Task, error)
	Save(task *Task) error
	FindAll() ([]Task, error)
	Update(task *Task) error
	Delete(id string) error
}
