package task

import "errors"

var ErrTaskNotFound = errors.New("task not found")

type Repoisitory interface {
	FindByID(id string) (*Task, error)
	Save(task *Task) error
	FindAll() ([]Task, error)
}
